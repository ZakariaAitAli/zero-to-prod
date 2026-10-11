package authn

import (
	"strings"
	"testing"
)

func lookupFrom(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func validOIDCEnvironment() map[string]string {
	return map[string]string{
		"AUTHENTICATION":  "oidc",
		"OIDC_ISSUER_URL": "https://auth.example/realms/work-items",
		"OIDC_AUDIENCE":   "work-items-api",
		"OIDC_JWKS_URL":   "https://auth.example/realms/work-items/protocol/openid-connect/certs",
	}
}

func TestLoadConfigRequiresExplicitMode(t *testing.T) {
	_, err := LoadConfig(lookupFrom(map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), "AUTHENTICATION is required") {
		t.Fatalf("expected an explicit-mode error, got %v", err)
	}

	_, err = LoadConfig(lookupFrom(map[string]string{"AUTHENTICATION": "optional"}))
	if err == nil {
		t.Fatal("unknown mode accepted")
	}
}

func TestLoadConfigDisabled(t *testing.T) {
	config, err := LoadConfig(lookupFrom(map[string]string{"AUTHENTICATION": "disabled"}))
	if err != nil || config.Mode != ModeDisabled {
		t.Fatalf("expected disabled mode, got %+v, %v", config, err)
	}
}

func TestLoadConfigOIDCDefaults(t *testing.T) {
	config, err := LoadConfig(lookupFrom(validOIDCEnvironment()))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(config.Algorithms) != 1 || config.Algorithms[0].String() != "RS256" {
		t.Fatalf("expected RS256 only, got %v", config.Algorithms)
	}
	if config.ClockSkew != defaultClockSkew {
		t.Fatalf("unexpected clock skew %v", config.ClockSkew)
	}
}

func TestLoadConfigRejectsUnsafeSettings(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		message string
	}{
		{"missing issuer", "OIDC_ISSUER_URL", "", "OIDC_ISSUER_URL is required"},
		{"http issuer", "OIDC_ISSUER_URL", "http://auth.example/realms/work-items", "absolute https URL"},
		{"missing JWKS", "OIDC_JWKS_URL", "", "OIDC_JWKS_URL is required"},
		{"missing audience", "OIDC_AUDIENCE", "", "OIDC_AUDIENCE is required"},
		{"symmetric algorithm", "OIDC_ALLOWED_ALGORITHMS", "RS256,HS256", "unsupported algorithm \"HS256\""},
		{"none algorithm", "OIDC_ALLOWED_ALGORITHMS", "none", "unsupported algorithm \"none\""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			environment := validOIDCEnvironment()
			environment[test.key] = test.value
			_, err := LoadConfig(lookupFrom(environment))
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("expected %q, got %v", test.message, err)
			}
		})
	}
}
