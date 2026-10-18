package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// certificateCheckInterval bounds how often a handshake rereads the
// certificate file.
const certificateCheckInterval = time.Second

// tlsConfigFromEnvironment returns the server's TLS configuration, or nil
// when TLS_CERT_FILE is unset (plain HTTP, as in local development).
//
// TLS_CERT_FILE names one PEM file holding the private key and the
// certificate, as the security lab mounts it (ADR 0008). The file is read at
// startup, so a missing or invalid file stops the process, and reread when
// it changes, so a renewed certificate is served to new connections without
// a restart. Session tickets are disabled: every new connection performs a
// full handshake and its client verifies the certificate served at that
// moment.
func tlsConfigFromEnvironment(lookup func(string) string) (*tls.Config, error) {
	path := strings.TrimSpace(lookup("TLS_CERT_FILE"))
	if path == "" {
		return nil, nil
	}

	source, err := newCertificateFile(path, time.Now)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		MinVersion:             tls.VersionTLS12,
		GetCertificate:         source.certificate,
		SessionTicketsDisabled: true,
	}, nil
}

// certificateFile serves the key pair in one PEM file and reloads it when
// the file's content changes.
//
// Last good certificate: when the file becomes unreadable, or changes to
// content that is not a valid key pair (for example a partial write), the
// pair already loaded stays in use, also after it expires. The server does
// not judge validity; its clients do, and a client that verifies (the
// ingress) refuses an expired or otherwise invalid certificate, so the
// failure is closed at the client. Each such event is logged once, and every
// load logs the serial and expiry now served, so the served certificate can
// be compared with the file. Writing a valid file recovers on the next
// handshake at least certificateCheckInterval after the previous check.
type certificateFile struct {
	path string
	now  func() time.Time

	mu        sync.Mutex
	current   *tls.Certificate
	content   []byte
	checkedAt time.Time
	rejected  []byte
	lastError string
}

func newCertificateFile(path string, now func() time.Time) (*certificateFile, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read TLS_CERT_FILE: %w", err)
	}

	pair, err := tls.X509KeyPair(content, content)
	if err != nil {
		return nil, fmt.Errorf("TLS_CERT_FILE %s does not hold a key and certificate: %w", path, err)
	}

	log.Printf("TLS certificate loaded from %s (%s)", path, describeCertificate(&pair))
	return &certificateFile{
		path:      path,
		now:       now,
		current:   &pair,
		content:   content,
		checkedAt: now(),
	}, nil
}

func (file *certificateFile) certificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	file.mu.Lock()
	defer file.mu.Unlock()

	if now := file.now(); now.Sub(file.checkedAt) >= certificateCheckInterval {
		file.checkedAt = now
		file.reload()
	}

	return file.current, nil
}

// reload is called with mu held.
func (file *certificateFile) reload() {
	content, err := os.ReadFile(file.path)
	if err != nil {
		file.report(fmt.Sprintf("TLS certificate file unreadable, keeping the current certificate (%s): %v",
			describeCertificate(file.current), err))
		return
	}

	if bytes.Equal(content, file.content) {
		file.lastError = ""
		return
	}
	if bytes.Equal(content, file.rejected) {
		return
	}

	pair, err := tls.X509KeyPair(content, content)
	if err != nil {
		file.rejected = content
		file.report(fmt.Sprintf("TLS certificate file changed but holds no valid key pair, keeping the current certificate (%s): %v",
			describeCertificate(file.current), err))
		return
	}

	file.current = &pair
	file.content = content
	file.rejected = nil
	file.lastError = ""
	log.Printf("TLS certificate reloaded from %s (%s)", file.path, describeCertificate(&pair))
}

// report logs a reload problem once, not on every check while it persists.
func (file *certificateFile) report(message string) {
	if message == file.lastError {
		return
	}
	file.lastError = message
	log.Print(message)
}

// describeCertificate names the leaf by serial and expiry; no key material.
func describeCertificate(pair *tls.Certificate) string {
	if pair == nil || len(pair.Certificate) == 0 {
		return "no certificate"
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return "unparsable certificate"
	}
	return fmt.Sprintf("serial %X, valid until %s", leaf.SerialNumber, leaf.NotAfter.UTC().Format(time.RFC3339))
}
