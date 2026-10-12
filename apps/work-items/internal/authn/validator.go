package authn

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

const (
	keyFetchTimeout      = 3 * time.Second
	unknownKeyRefreshGap = 10 * time.Second
)

// ErrUnavailable means the issuer's signing keys could not be obtained, so a
// token cannot be judged. Callers should report a temporary failure, not an
// authentication failure.
var ErrUnavailable = errors.New("signing keys unavailable")

// unavailable keeps the fetch failure for logs; callers match ErrUnavailable
// with errors.Is.
func unavailable(cause error) error {
	return fmt.Errorf("%w: %v", ErrUnavailable, cause)
}

// Failure is a token that was examined and rejected. Category names the
// reason for logs; it never contains token material.
type Failure struct {
	Category string
}

func (f *Failure) Error() string {
	return "token rejected: " + f.Category
}

// Principal is the authenticated caller.
type Principal struct {
	Subject string
	// AuthorizedParty is the client the token was issued to (azp).
	AuthorizedParty string
}

// Validator verifies access tokens against the issuer's published keys.
// Validation is local: it does not consult the identity provider per request.
type Validator struct {
	config Config
	cache  *jwk.Cache
	clock  jwt.Clock

	// refreshMu serialises explicit refreshes and guards their last outcome.
	refreshMu      sync.Mutex
	lastRefresh    time.Time
	lastRefreshErr error
	lastKeys       jwk.Set
}

// NewValidator registers the JWKS URL without waiting for the first fetch, so
// an unreachable identity provider does not prevent the API from starting.
func NewValidator(ctx context.Context, config Config) (*Validator, error) {
	if config.Mode != ModeOIDC {
		return nil, fmt.Errorf("validator requires AUTHENTICATION=%s", ModeOIDC)
	}

	client, err := httpClient(config.CAFile)
	if err != nil {
		return nil, err
	}

	cache, err := jwk.NewCache(ctx, httprc.NewClient())
	if err != nil {
		return nil, fmt.Errorf("create JWKS cache: %w", err)
	}

	// The HTTP client is set per resource: Register otherwise substitutes
	// jwx's default fetch client, which does not trust OIDC_CA_FILE.
	if err := cache.Register(ctx, config.JWKSURL,
		jwk.WithHTTPClient(client),
		jwk.WithWaitReady(false),
	); err != nil {
		return nil, fmt.Errorf("register JWKS URL: %w", err)
	}

	return &Validator{config: config, cache: cache, clock: jwt.ClockFunc(time.Now)}, nil
}

func httpClient(caFile string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read OIDC_CA_FILE: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("OIDC_CA_FILE %s contains no certificates", caFile)
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}

	return &http.Client{Transport: transport, Timeout: keyFetchTimeout}, nil
}

// Validate verifies a compact JWS access token and returns its principal.
// Errors are *Failure (rejected token) or wrap ErrUnavailable (cannot judge).
func (v *Validator) Validate(ctx context.Context, raw string) (Principal, error) {
	message, err := jws.ParseString(raw)
	if err != nil || len(message.Signatures()) != 1 {
		return Principal{}, &Failure{Category: "malformed"}
	}

	headers := message.Signatures()[0].ProtectedHeaders()
	algorithm, ok := headers.Algorithm()
	if !ok || !v.allowed(algorithm.String()) {
		return Principal{}, &Failure{Category: "algorithm"}
	}

	keys, err := v.keys(ctx)
	if err != nil {
		return Principal{}, unavailable(err)
	}

	keyID, _ := headers.KeyID()
	if _, found := keys.LookupKeyID(keyID); !found {
		// A new signing key may have been published. Only a successful
		// refresh that still lacks the key is a rejection; a failed refresh
		// leaves the token undecided.
		refreshed, err := v.refreshForUnknownKey(ctx)
		if err != nil {
			return Principal{}, unavailable(err)
		}
		if _, found := refreshed.LookupKeyID(keyID); !found {
			return Principal{}, &Failure{Category: "unknown_key"}
		}
		keys = refreshed
	}

	token, err := jwt.ParseString(raw,
		jwt.WithKeySet(keys, jws.WithInferAlgorithmFromKey(false)),
		jwt.WithValidate(true),
		jwt.WithIssuer(v.config.Issuer),
		jwt.WithAudience(v.config.Audience),
		jwt.WithRequiredClaim("exp"),
		jwt.WithAcceptableSkew(v.config.ClockSkew),
		jwt.WithClock(v.clock),
	)
	if err != nil {
		return Principal{}, &Failure{Category: classify(err)}
	}

	principal := Principal{}
	principal.Subject, _ = token.Subject()
	var azp string
	if err := token.Get("azp", &azp); err == nil {
		principal.AuthorizedParty = azp
	}
	return principal, nil
}

func (v *Validator) allowed(name string) bool {
	for _, algorithm := range v.config.Algorithms {
		if algorithm.String() == name {
			return true
		}
	}
	return false
}

func (v *Validator) keys(ctx context.Context) (jwk.Set, error) {
	ctx, cancel := context.WithTimeout(ctx, keyFetchTimeout)
	defer cancel()

	if keys, err := v.cache.Lookup(ctx, v.config.JWKSURL); err == nil && keys.Len() > 0 {
		return keys, nil
	}
	return v.refresh(ctx)
}

func (v *Validator) refresh(ctx context.Context) (jwk.Set, error) {
	v.refreshMu.Lock()
	defer v.refreshMu.Unlock()
	return v.refreshLocked(ctx)
}

// refreshLocked fetches the key set and records the outcome. The caller holds
// refreshMu.
func (v *Validator) refreshLocked(ctx context.Context) (jwk.Set, error) {
	v.lastRefresh = time.Now()

	keys, err := v.cache.Refresh(ctx, v.config.JWKSURL)
	if err == nil && keys.Len() == 0 {
		err = errors.New("empty JWKS")
	}
	v.lastRefreshErr = err
	if err != nil {
		return nil, err
	}
	v.lastKeys = keys
	return keys, nil
}

// refreshForUnknownKey fetches the key set at most once per cooldown. The
// cooldown check and the fetch happen under one lock, so concurrent requests
// for an unknown key share a single refresh. Within the cooldown the previous
// outcome is reused: the last fetched set if it succeeded, its error if not.
func (v *Validator) refreshForUnknownKey(ctx context.Context) (jwk.Set, error) {
	v.refreshMu.Lock()
	defer v.refreshMu.Unlock()

	if !v.lastRefresh.IsZero() && time.Since(v.lastRefresh) < unknownKeyRefreshGap {
		if v.lastRefreshErr != nil {
			return nil, v.lastRefreshErr
		}
		return v.lastKeys, nil
	}

	ctx, cancel := context.WithTimeout(ctx, keyFetchTimeout)
	defer cancel()
	return v.refreshLocked(ctx)
}

func classify(err error) string {
	switch {
	case errors.Is(err, jwt.TokenExpiredError()):
		return "expired"
	case errors.Is(err, jwt.TokenNotYetValidError()):
		return "not_yet_valid"
	case errors.Is(err, jwt.InvalidIssuerError()):
		return "issuer"
	case errors.Is(err, jwt.InvalidAudienceError()):
		return "audience"
	case errors.Is(err, jws.VerificationError()):
		return "signature"
	case errors.Is(err, jwt.ValidateError()):
		return "claims"
	default:
		return "invalid"
	}
}
