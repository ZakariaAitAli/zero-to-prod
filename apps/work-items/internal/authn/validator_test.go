package authn

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZakariaAitAli/zero-to-prod/apps/work-items/internal/authz"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

const (
	testIssuer   = "https://issuer.example/realms/test"
	testAudience = "work-items-api"
)

type signingKey struct {
	private jwk.Key
	public  jwk.Key
}

func newSigningKey(t *testing.T, kid string) signingKey {
	t.Helper()
	raw, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	private, err := jwk.Import(raw)
	if err != nil {
		t.Fatalf("import key: %v", err)
	}
	for _, key := range []jwk.Key{private} {
		_ = key.Set(jwk.KeyIDKey, kid)
		_ = key.Set(jwk.AlgorithmKey, jwa.RS256())
	}
	public, err := private.PublicKey()
	if err != nil {
		t.Fatalf("public key: %v", err)
	}
	return signingKey{private: private, public: public}
}

// jwksServer publishes a mutable key set and can be made unavailable.
type jwksServer struct {
	mu          sync.Mutex
	keys        []jwk.Key
	unavailable bool
	requests    int
	server      *httptest.Server
}

func newJWKSServer(t *testing.T, keys ...jwk.Key) *jwksServer {
	t.Helper()
	s := &jwksServer{keys: keys}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.requests++
		if s.unavailable {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		set := jwk.NewSet()
		for _, key := range s.keys {
			_ = set.AddKey(key)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(set)
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *jwksServer) set(unavailable bool, keys ...jwk.Key) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unavailable = unavailable
	if keys != nil {
		s.keys = keys
	}
}

func (s *jwksServer) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

// expireCooldown moves the validator outside the unknown-key refresh cooldown.
func expireCooldown(validator *Validator) {
	validator.refreshMu.Lock()
	validator.lastRefresh = time.Now().Add(-unknownKeyRefreshGap - time.Second)
	validator.refreshMu.Unlock()
}

func newTestValidator(t *testing.T, url string) *Validator {
	t.Helper()
	validator, err := NewValidator(context.Background(), Config{
		Mode:       ModeOIDC,
		Issuer:     testIssuer,
		Audience:   testAudience,
		JWKSURL:    url,
		Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()},
		ClockSkew:  30 * time.Second,
	})
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}
	return validator
}

type claims struct {
	issuer    string
	audience  string
	expiresIn time.Duration
	notBefore time.Duration
	noExpiry  bool
	// resourceAccess, when set, becomes the resource_access claim.
	resourceAccess any
}

func validClaims() claims {
	return claims{issuer: testIssuer, audience: testAudience, expiresIn: 5 * time.Minute}
}

func sign(t *testing.T, key jwk.Key, algorithm jwa.SignatureAlgorithm, c claims) string {
	t.Helper()
	now := time.Now()
	builder := jwt.NewBuilder().
		Issuer(c.issuer).
		Audience([]string{c.audience}).
		Subject("user-1").
		IssuedAt(now).
		Claim("azp", "work-items-gateway")
	if !c.noExpiry {
		builder = builder.Expiration(now.Add(c.expiresIn))
	}
	if c.notBefore != 0 {
		builder = builder.NotBefore(now.Add(c.notBefore))
	}
	if c.resourceAccess != nil {
		builder = builder.Claim("resource_access", c.resourceAccess)
	}
	token, err := builder.Build()
	if err != nil {
		t.Fatalf("build token: %v", err)
	}
	signed, err := jwt.Sign(token, jwt.WithKey(algorithm, key))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return string(signed)
}

func expectFailure(t *testing.T, err error, category string) {
	t.Helper()
	var failure *Failure
	if !errors.As(err, &failure) {
		t.Fatalf("expected failure %q, got %v", category, err)
	}
	if failure.Category != category {
		t.Fatalf("expected failure %q, got %q", category, failure.Category)
	}
}

func TestValidateAcceptsValidToken(t *testing.T) {
	key := newSigningKey(t, "k1")
	server := newJWKSServer(t, key.public)
	validator := newTestValidator(t, server.server.URL)

	principal, err := validator.Validate(context.Background(), sign(t, key.private, jwa.RS256(), validClaims()))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if principal.Subject != "user-1" || principal.AuthorizedParty != "work-items-gateway" {
		t.Fatalf("unexpected principal %+v", principal)
	}
}

func TestValidateReadsPermissionsFromSignedClaim(t *testing.T) {
	key := newSigningKey(t, "k1")
	server := newJWKSServer(t, key.public)
	validator := newTestValidator(t, server.server.URL)

	tests := []struct {
		name           string
		resourceAccess any
		want           []string
		state          authz.Claim
	}{
		{"granted", map[string]any{"work-items-api": map[string]any{"roles": []any{"items.read", "items.process"}}},
			[]string{"items.process", "items.read"}, authz.ClaimPresent},
		{"non-string entry grants nothing", map[string]any{"work-items-api": map[string]any{"roles": []any{"items.read", 1}}},
			[]string{}, authz.ClaimMalformed},
		{"other client only", map[string]any{"work-items-gateway": map[string]any{"roles": []any{"items.create"}}},
			[]string{}, authz.ClaimAbsent},
		{"no claim", nil, []string{}, authz.ClaimAbsent},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := validClaims()
			c.resourceAccess = test.resourceAccess
			principal, err := validator.Validate(context.Background(), sign(t, key.private, jwa.RS256(), c))
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
			if principal.RoleClaim != test.state {
				t.Fatalf("role claim = %s, want %s", principal.RoleClaim, test.state)
			}
			if got := principal.Permissions.Names(); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("permissions = %v, want %v", got, test.want)
			}
		})
	}
}

func TestValidateRejectsInvalidTokens(t *testing.T) {
	key := newSigningKey(t, "k1")
	other := newSigningKey(t, "k1") // same kid, different key
	stranger := newSigningKey(t, "k-unknown")
	server := newJWKSServer(t, key.public)
	validator := newTestValidator(t, server.server.URL)

	expired := validClaims()
	expired.expiresIn = -2 * time.Minute
	future := validClaims()
	future.notBefore = 2 * time.Minute
	wrongIssuer := validClaims()
	wrongIssuer.issuer = "https://other.example/realms/test"
	wrongAudience := validClaims()
	wrongAudience.audience = "account"
	noExpiry := validClaims()
	noExpiry.noExpiry = true

	var rawPublic rsa.PublicKey
	if err := jwk.Export(key.public, &rawPublic); err != nil {
		t.Fatalf("export public key: %v", err)
	}
	hmacKey, err := jwk.Import([]byte(rawPublic.N.String()))
	if err != nil {
		t.Fatalf("import hmac key: %v", err)
	}
	_ = hmacKey.Set(jwk.KeyIDKey, "k1")

	tests := []struct {
		name     string
		token    string
		category string
	}{
		{"malformed", "not-a-token", "malformed"},
		{"expired", sign(t, key.private, jwa.RS256(), expired), "expired"},
		{"not yet valid", sign(t, key.private, jwa.RS256(), future), "not_yet_valid"},
		{"wrong issuer", sign(t, key.private, jwa.RS256(), wrongIssuer), "issuer"},
		{"wrong audience", sign(t, key.private, jwa.RS256(), wrongAudience), "audience"},
		{"missing exp", sign(t, key.private, jwa.RS256(), noExpiry), "claims"},
		{"wrong key, same kid", sign(t, other.private, jwa.RS256(), validClaims()), "signature"},
		{"unknown kid", sign(t, stranger.private, jwa.RS256(), validClaims()), "unknown_key"},
		{"algorithm not allowed", sign(t, key.private, jwa.RS384(), validClaims()), "algorithm"},
		{"HMAC with public key material", sign(t, hmacKey, jwa.HS256(), validClaims()), "algorithm"},
		{"alg none", unsignedToken(t), "algorithm"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validator.Validate(context.Background(), test.token)
			expectFailure(t, err, test.category)
		})
	}
}

// unsignedToken builds an "alg":"none" token by hand: header.payload. (empty
// signature).
func unsignedToken(t *testing.T) string {
	t.Helper()
	header := `{"alg":"none","typ":"JWT","kid":"k1"}`
	payload := `{"iss":"` + testIssuer + `","aud":"` + testAudience + `","sub":"user-1","exp":` +
		strconv.FormatInt(time.Now().Add(5*time.Minute).Unix(), 10) + `}`
	encode := base64.RawURLEncoding.EncodeToString
	return encode([]byte(header)) + "." + encode([]byte(payload)) + "."
}

// The issuer's certificate is signed by a private CA, as in the security lab:
// keys are fetched only when OIDC_CA_FILE is trusted.
func TestValidateFetchesKeysOverTLSWithConfiguredCA(t *testing.T) {
	key := newSigningKey(t, "k1")
	set := jwk.NewSet()
	_ = set.AddKey(key.public)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(set)
	}))
	t.Cleanup(server.Close)

	caFile := filepath.Join(t.TempDir(), "ca.crt")
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caFile, certificate, 0o600); err != nil {
		t.Fatalf("write CA file: %v", err)
	}

	newValidator := func(caFile string) *Validator {
		validator, err := NewValidator(context.Background(), Config{
			Mode:       ModeOIDC,
			Issuer:     testIssuer,
			Audience:   testAudience,
			JWKSURL:    server.URL,
			Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()},
			CAFile:     caFile,
			ClockSkew:  30 * time.Second,
		})
		if err != nil {
			t.Fatalf("new validator: %v", err)
		}
		return validator
	}
	token := sign(t, key.private, jwa.RS256(), validClaims())

	if _, err := newValidator(caFile).Validate(context.Background(), token); err != nil {
		t.Fatalf("validate with configured CA: %v", err)
	}

	// Control: without the CA the same server is not trusted.
	_, err := newValidator("").Validate(context.Background(), token)
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("expected an untrusted-certificate ErrUnavailable, got %v", err)
	}
}

func TestValidateReportsUnavailableKeys(t *testing.T) {
	key := newSigningKey(t, "k1")
	server := newJWKSServer(t, key.public)
	server.set(true)
	validator := newTestValidator(t, server.server.URL)

	_, err := validator.Validate(context.Background(), sign(t, key.private, jwa.RS256(), validClaims()))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}

func TestValidateUsesCachedKeysWhenIssuerIsDown(t *testing.T) {
	key := newSigningKey(t, "k1")
	server := newJWKSServer(t, key.public)
	validator := newTestValidator(t, server.server.URL)
	token := sign(t, key.private, jwa.RS256(), validClaims())

	if _, err := validator.Validate(context.Background(), token); err != nil {
		t.Fatalf("validate while available: %v", err)
	}
	server.set(true)
	if _, err := validator.Validate(context.Background(), token); err != nil {
		t.Fatalf("validate with cached keys: %v", err)
	}
}

func TestValidateRefreshesForRotatedKey(t *testing.T) {
	first := newSigningKey(t, "k1")
	second := newSigningKey(t, "k2")
	server := newJWKSServer(t, first.public)
	validator := newTestValidator(t, server.server.URL)

	if _, err := validator.Validate(context.Background(), sign(t, first.private, jwa.RS256(), validClaims())); err != nil {
		t.Fatalf("validate first key: %v", err)
	}

	server.set(false, first.public, second.public)
	validator.refreshMu.Lock()
	validator.lastRefresh = time.Time{} // outside the refresh rate limit
	validator.refreshMu.Unlock()

	if _, err := validator.Validate(context.Background(), sign(t, second.private, jwa.RS256(), validClaims())); err != nil {
		t.Fatalf("validate rotated key: %v", err)
	}
}

// primedValidator returns a validator that has fetched and accepted key.
func primedValidator(t *testing.T, server *jwksServer, key signingKey) *Validator {
	t.Helper()
	validator := newTestValidator(t, server.server.URL)
	if _, err := validator.Validate(context.Background(), sign(t, key.private, jwa.RS256(), validClaims())); err != nil {
		t.Fatalf("prime validator: %v", err)
	}
	// Priming may have refreshed explicitly if the background fetch had not
	// finished; start every test outside the cooldown.
	expireCooldown(validator)
	return validator
}

func TestValidateRejectsUnknownKeyAfterSuccessfulRefresh(t *testing.T) {
	key := newSigningKey(t, "k1")
	stranger := newSigningKey(t, "k-unknown")
	server := newJWKSServer(t, key.public)
	validator := primedValidator(t, server, key)
	unknown := sign(t, stranger.private, jwa.RS256(), validClaims())

	before := server.requestCount()
	_, err := validator.Validate(context.Background(), unknown)
	expectFailure(t, err, "unknown_key")
	if got := server.requestCount() - before; got != 1 {
		t.Fatalf("expected one refresh, got %d", got)
	}

	// Within the cooldown the successful result is reused without a fetch.
	_, err = validator.Validate(context.Background(), unknown)
	expectFailure(t, err, "unknown_key")
	if got := server.requestCount() - before; got != 1 {
		t.Fatalf("expected no refresh within cooldown, got %d", got-1)
	}
}

func TestValidateReportsUnavailableWhenUnknownKeyRefreshFails(t *testing.T) {
	key := newSigningKey(t, "k1")
	stranger := newSigningKey(t, "k-unknown")
	server := newJWKSServer(t, key.public)
	validator := primedValidator(t, server, key)
	known := sign(t, key.private, jwa.RS256(), validClaims())
	unknown := sign(t, stranger.private, jwa.RS256(), validClaims())

	server.set(true)
	before := server.requestCount()
	if _, err := validator.Validate(context.Background(), unknown); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("failed refresh: expected ErrUnavailable, got %v", err)
	}
	if server.requestCount()-before == 0 {
		t.Fatalf("expected a refresh attempt")
	}

	// Within the cooldown the failure is reused: still undecided, no fetch.
	attempts := server.requestCount()
	if _, err := validator.Validate(context.Background(), unknown); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("cooldown after failure: expected ErrUnavailable, got %v", err)
	}
	if server.requestCount() != attempts {
		t.Fatalf("expected no refresh within cooldown after failure")
	}

	// Known keys remain usable from the cache during the outage.
	if _, err := validator.Validate(context.Background(), known); err != nil {
		t.Fatalf("known key during outage: %v", err)
	}

	// Recovery inside the cooldown is not observed until the cooldown ends.
	server.set(false)
	if _, err := validator.Validate(context.Background(), unknown); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("recovered issuer within cooldown: expected ErrUnavailable, got %v", err)
	}

	expireCooldown(validator)
	_, err := validator.Validate(context.Background(), unknown)
	expectFailure(t, err, "unknown_key")
}

func TestValidateSharesOneRefreshAcrossConcurrentUnknownKeys(t *testing.T) {
	key := newSigningKey(t, "k1")
	stranger := newSigningKey(t, "k-unknown")
	server := newJWKSServer(t, key.public)
	validator := primedValidator(t, server, key)
	unknown := sign(t, stranger.private, jwa.RS256(), validClaims())

	// A check-then-fetch race has a window of a few instructions, so repeat the
	// burst; each round starts outside the cooldown and must cause exactly one
	// fetch. Detection is probabilistic: against the previous check-then-fetch
	// shape one run failed 17 times in 20. CI repeats this test.
	const (
		rounds  = 1000
		callers = 32
	)
	for round := 0; round < rounds; round++ {
		expireCooldown(validator)
		before := server.requestCount()
		start := make(chan struct{})
		errs := make(chan error, callers)
		var wg sync.WaitGroup
		for i := 0; i < callers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, err := validator.Validate(context.Background(), unknown)
				errs <- err
			}()
		}
		close(start)
		wg.Wait()
		close(errs)

		for err := range errs {
			expectFailure(t, err, "unknown_key")
		}
		if got := server.requestCount() - before; got != 1 {
			t.Fatalf("round %d: expected one refresh for %d concurrent requests, got %d", round, callers, got)
		}
	}
}
