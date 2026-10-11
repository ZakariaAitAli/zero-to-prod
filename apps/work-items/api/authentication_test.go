package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZakariaAitAli/zero-to-prod/apps/work-items/internal/authn"
)

type fakeValidator struct {
	err       error
	principal authn.Principal
	seen      string
}

func (f *fakeValidator) Validate(_ context.Context, raw string) (authn.Principal, error) {
	f.seen = raw
	return f.principal, f.err
}

func authenticatedHandler(validator tokenValidator) http.Handler {
	readiness := &readinessState{}
	readiness.set(true)
	return newApplicationHandler("test", readiness, &stubApplicationStore{}, requireBearerToken(validator))
}

func TestAuthenticationLeavesOperationalRoutesPublic(t *testing.T) {
	handler := authenticatedHandler(&fakeValidator{err: &authn.Failure{Category: "signature"}})

	for _, path := range []string{"/health", "/ready", "/version"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", path, recorder.Code)
		}
	}
}

func TestAuthenticationRejectsMissingToken(t *testing.T) {
	validator := &fakeValidator{}
	handler := authenticatedHandler(validator)

	for _, header := range []string{"", "Basic dXNlcjpwYXNz", "Bearer ", "Bearer"} {
		request := httptest.NewRequest(http.MethodGet, "/items", nil)
		if header != "" {
			request.Header.Set("Authorization", header)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%q: expected 401, got %d", header, recorder.Code)
		}
		if got := recorder.Header().Get("WWW-Authenticate"); got != `Bearer realm="work-items"` {
			t.Fatalf("%q: unexpected WWW-Authenticate %q", header, got)
		}
		if !strings.Contains(recorder.Body.String(), `"unauthenticated"`) {
			t.Fatalf("%q: unexpected body %s", header, recorder.Body.String())
		}
	}
	if validator.seen != "" {
		t.Fatalf("validator called without a bearer token")
	}
}

func TestAuthenticationRejectsInvalidToken(t *testing.T) {
	handler := authenticatedHandler(&fakeValidator{err: &authn.Failure{Category: "audience"}})

	request := httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(`{"title":"x"}`))
	request.Header.Set("Authorization", "Bearer token-value")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", recorder.Code)
	}
	if got := recorder.Header().Get("WWW-Authenticate"); !strings.Contains(got, `error="invalid_token"`) {
		t.Fatalf("unexpected WWW-Authenticate %q", got)
	}
}

func TestAuthenticationReportsUnavailableKeys(t *testing.T) {
	handler := authenticatedHandler(&fakeValidator{err: authn.ErrUnavailable})

	request := httptest.NewRequest(http.MethodGet, "/items", nil)
	request.Header.Set("Authorization", "Bearer token-value")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"authentication_unavailable"`) {
		t.Fatalf("unexpected body %s", recorder.Body.String())
	}
	if recorder.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("503 must not ask the client to re-authenticate")
	}
}

func TestAuthenticationPassesValidToken(t *testing.T) {
	validator := &fakeValidator{principal: authn.Principal{Subject: "user-1"}}
	handler := authenticatedHandler(validator)

	request := httptest.NewRequest(http.MethodGet, "/items", nil)
	request.Header.Set("Authorization", "bearer token-value")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if validator.seen != "token-value" {
		t.Fatalf("validator received %q", validator.seen)
	}
}
