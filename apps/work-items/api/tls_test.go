package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

func newTestCA(t *testing.T) testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return testCA{cert: cert, key: key, pool: pool}
}

// pemPair returns a key and a certificate for name, signed by the CA, in one
// PEM block sequence (the layout the lab mounts).
func (ca testCA) pemPair(t *testing.T, name string, serial int64) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	out := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
}

// replaceFile writes content next to path and renames it into place, as the
// lab tool does.
func replaceFile(t *testing.T, path string, content []byte) {
	t.Helper()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *fakeClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *fakeClock) advance(d time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(d)
}

func servedSerial(t *testing.T, config *tls.Config) int64 {
	t.Helper()
	cert, err := config.GetCertificate(&tls.ClientHelloInfo{ServerName: "api"})
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	return leaf.SerialNumber.Int64()
}

func TestTLSConfigFromEnvironmentUnsetMeansPlainHTTP(t *testing.T) {
	config, err := tlsConfigFromEnvironment(func(string) string { return "" })
	if err != nil || config != nil {
		t.Fatalf("expected no TLS configuration, got %v, %v", config, err)
	}
}

func TestTLSConfigFromEnvironmentRefusesMissingOrInvalidFile(t *testing.T) {
	dir := t.TempDir()
	invalid := filepath.Join(dir, "invalid.pem")
	if err := os.WriteFile(invalid, []byte("not a key pair"), 0o644); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"missing": filepath.Join(dir, "absent.pem"),
		"invalid": invalid,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := tlsConfigFromEnvironment(func(key string) string {
				if key == "TLS_CERT_FILE" {
					return path
				}
				return ""
			})
			if err == nil || !strings.Contains(err.Error(), "TLS_CERT_FILE") {
				t.Fatalf("expected an error naming TLS_CERT_FILE, got %v", err)
			}
		})
	}
}

func TestTLSConfigRequiresTLS12AndDisablesSessionTickets(t *testing.T) {
	ca := newTestCA(t)
	path := filepath.Join(t.TempDir(), "tls.pem")
	replaceFile(t, path, ca.pemPair(t, "api", 2))

	config, err := tlsConfigFromEnvironment(func(string) string { return path })
	if err != nil {
		t.Fatal(err)
	}
	if config.MinVersion != tls.VersionTLS12 {
		t.Fatalf("expected TLS 1.2 minimum, got %x", config.MinVersion)
	}
	if !config.SessionTicketsDisabled {
		t.Fatal("expected session tickets to be disabled")
	}
}

func TestCertificateFileReloadsChangedFileAndKeepsLastValidPair(t *testing.T) {
	ca := newTestCA(t)
	path := filepath.Join(t.TempDir(), "tls.pem")
	replaceFile(t, path, ca.pemPair(t, "api", 2))

	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	source, err := newCertificateFile(path, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	config := &tls.Config{GetCertificate: source.certificate}

	if got := servedSerial(t, config); got != 2 {
		t.Fatalf("expected serial 2, got %d", got)
	}

	replaceFile(t, path, ca.pemPair(t, "api", 3))
	if got := servedSerial(t, config); got != 2 {
		t.Fatalf("expected serial 2 until the check interval passed, got %d", got)
	}
	clock.advance(certificateCheckInterval)
	if got := servedSerial(t, config); got != 3 {
		t.Fatalf("expected the renewed serial 3, got %d", got)
	}

	replaceFile(t, path, []byte("truncated"))
	clock.advance(certificateCheckInterval)
	if got := servedSerial(t, config); got != 3 {
		t.Fatalf("expected an invalid replacement to keep serial 3, got %d", got)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	clock.advance(certificateCheckInterval)
	if got := servedSerial(t, config); got != 3 {
		t.Fatalf("expected a missing file to keep serial 3, got %d", got)
	}

	replaceFile(t, path, ca.pemPair(t, "api", 4))
	clock.advance(certificateCheckInterval)
	if got := servedSerial(t, config); got != 4 {
		t.Fatalf("expected recovery to serial 4, got %d", got)
	}
}

func TestCertificateFileReportsAPersistentProblemOnceAndLogsServedSerials(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	ca := newTestCA(t)
	path := filepath.Join(t.TempDir(), "tls.pem")
	replaceFile(t, path, ca.pemPair(t, "api", 0x2a))

	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	source, err := newCertificateFile(path, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	config := &tls.Config{GetCertificate: source.certificate}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		clock.advance(certificateCheckInterval)
		servedSerial(t, config)
	}
	if got := strings.Count(logs.String(), "unreadable"); got != 1 {
		t.Fatalf("expected one unreadable report for a persistent problem, got %d:\n%s", got, logs.String())
	}
	if !strings.Contains(logs.String(), "keeping the current certificate (serial 2A") {
		t.Fatalf("expected the report to name the serial still served:\n%s", logs.String())
	}

	replaceFile(t, path, ca.pemPair(t, "api", 0x2b))
	clock.advance(certificateCheckInterval)
	if got := servedSerial(t, config); got != 0x2b {
		t.Fatalf("expected recovery to serial 0x2b, got %x", got)
	}
	if !strings.Contains(logs.String(), "TLS certificate reloaded from "+path+" (serial 2B") {
		t.Fatalf("expected the reload to name the new serial:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), "PRIVATE KEY") {
		t.Fatal("log output contains key material")
	}
}

// startTLSServer runs the application server with TLS on a loopback port and
// returns its address.
func startTLSServer(t *testing.T, path string) string {
	t.Helper()
	config, err := tlsConfigFromEnvironment(func(string) string { return path })
	if err != nil {
		t.Fatal(err)
	}

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := probe.Addr().String()
	probe.Close()

	readiness := &readinessState{}
	server := newHTTPServer(address, newHandler("test-version", readiness, availableDatabase()))
	server.TLSConfig = config

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runHTTPServer(ctx, server, readiness, time.Second) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	deadline := time.Now().Add(2 * time.Second)
	for !readiness.isReady() {
		if time.Now().After(deadline) {
			t.Fatal("TLS server did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return address
}

func TestRunHTTPServerServesOnlyTLSAndClientsVerifyName(t *testing.T) {
	ca := newTestCA(t)
	path := filepath.Join(t.TempDir(), "tls.pem")
	replaceFile(t, path, ca.pemPair(t, "api", 2))
	address := startTLSServer(t, path)

	client := func(name string) *http.Client {
		return &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: ca.pool, ServerName: name},
		}}
	}

	response, err := client("api").Get("https://" + address + "/health")
	if err != nil {
		t.Fatalf("verified HTTPS request failed: %v", err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "healthy") {
		t.Fatalf("expected 200 healthy, got %d %q", response.StatusCode, body)
	}

	if _, err := client("oauth2-proxy").Get("https://" + address + "/health"); err == nil {
		t.Fatal("expected a client dialing another name to refuse the certificate")
	}

	untrusted := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{ServerName: "api", RootCAs: newTestCA(t).pool},
	}}
	if _, err := untrusted.Get("https://" + address + "/health"); err == nil {
		t.Fatal("expected a client trusting another CA to refuse the certificate")
	}

	// A plaintext request reaches no handler: Go answers 400 and closes.
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.WriteString(conn, "GET /health HTTP/1.1\r\nHost: api\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	reply, _ := io.ReadAll(bufio.NewReader(conn))
	if strings.Contains(string(reply), "healthy") || !strings.Contains(string(reply), "400") {
		t.Fatalf("expected a plaintext request to be refused with 400, got %q", reply)
	}
}

func TestTLSServerDoesNotResumeSessions(t *testing.T) {
	ca := newTestCA(t)
	path := filepath.Join(t.TempDir(), "tls.pem")
	replaceFile(t, path, ca.pemPair(t, "api", 2))
	address := startTLSServer(t, path)

	cache := tls.NewLRUClientSessionCache(4)
	for attempt := 0; attempt < 2; attempt++ {
		conn, err := tls.Dial("tcp", address, &tls.Config{
			RootCAs: ca.pool, ServerName: "api", ClientSessionCache: cache,
		})
		if err != nil {
			t.Fatal(err)
		}
		// Read the post-handshake messages (where a TLS 1.3 ticket would be).
		_, _ = io.WriteString(conn, "GET /health HTTP/1.1\r\nHost: api\r\nConnection: close\r\n\r\n")
		_, _ = io.ReadAll(conn)
		if conn.ConnectionState().DidResume {
			t.Fatalf("connection %d resumed a session", attempt+1)
		}
		conn.Close()
	}
}
