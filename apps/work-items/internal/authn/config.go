// Package authn validates OAuth 2.0 bearer access tokens issued by an OpenID
// Connect identity provider.
package authn

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
)

// Mode selects whether requests must carry a valid bearer token.
type Mode string

const (
	// ModeDisabled performs no authentication. Development only.
	ModeDisabled Mode = "disabled"
	// ModeOIDC requires a valid access token from the configured issuer.
	ModeOIDC Mode = "oidc"
)

const defaultClockSkew = 30 * time.Second

// Config holds the validation policy. All fields except CAFile are required
// in ModeOIDC.
type Config struct {
	Mode       Mode
	Issuer     string
	Audience   string
	JWKSURL    string
	Algorithms []jwa.SignatureAlgorithm
	// CAFile, when set, replaces the system roots for JWKS requests.
	CAFile    string
	ClockSkew time.Duration
}

// permittedAlgorithms are the asymmetric algorithms an allow-list may name.
// Symmetric (HS*) algorithms and "none" are never accepted: the validator
// verifies with the issuer's public keys.
var permittedAlgorithms = map[string]jwa.SignatureAlgorithm{
	"RS256": jwa.RS256(), "RS384": jwa.RS384(), "RS512": jwa.RS512(),
	"PS256": jwa.PS256(), "PS384": jwa.PS384(), "PS512": jwa.PS512(),
	"ES256": jwa.ES256(), "ES384": jwa.ES384(), "ES512": jwa.ES512(),
	"EdDSA": jwa.EdDSA(),
}

// LoadConfig reads the policy from the environment. AUTHENTICATION must be
// set explicitly; there is no default mode.
func LoadConfig(lookup func(string) string) (Config, error) {
	mode := Mode(strings.TrimSpace(lookup("AUTHENTICATION")))

	switch mode {
	case ModeDisabled:
		return Config{Mode: ModeDisabled}, nil
	case ModeOIDC:
	case "":
		return Config{}, fmt.Errorf("AUTHENTICATION is required (%q or %q)", ModeDisabled, ModeOIDC)
	default:
		return Config{}, fmt.Errorf("AUTHENTICATION must be %q or %q", ModeDisabled, ModeOIDC)
	}

	config := Config{
		Mode:      ModeOIDC,
		Issuer:    strings.TrimSpace(lookup("OIDC_ISSUER_URL")),
		Audience:  strings.TrimSpace(lookup("OIDC_AUDIENCE")),
		JWKSURL:   strings.TrimSpace(lookup("OIDC_JWKS_URL")),
		CAFile:    strings.TrimSpace(lookup("OIDC_CA_FILE")),
		ClockSkew: defaultClockSkew,
	}

	for name, value := range map[string]string{
		"OIDC_ISSUER_URL": config.Issuer,
		"OIDC_JWKS_URL":   config.JWKSURL,
	} {
		if value == "" {
			return Config{}, fmt.Errorf("%s is required when AUTHENTICATION=%s", name, ModeOIDC)
		}
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return Config{}, fmt.Errorf("%s must be an absolute https URL", name)
		}
	}

	if config.Audience == "" {
		return Config{}, fmt.Errorf("OIDC_AUDIENCE is required when AUTHENTICATION=%s", ModeOIDC)
	}

	names := strings.TrimSpace(lookup("OIDC_ALLOWED_ALGORITHMS"))
	if names == "" {
		names = "RS256"
	}
	for _, name := range strings.Split(names, ",") {
		algorithm, ok := permittedAlgorithms[strings.TrimSpace(name)]
		if !ok {
			return Config{}, fmt.Errorf("OIDC_ALLOWED_ALGORITHMS: unsupported algorithm %q", strings.TrimSpace(name))
		}
		config.Algorithms = append(config.Algorithms, algorithm)
	}

	return config, nil
}
