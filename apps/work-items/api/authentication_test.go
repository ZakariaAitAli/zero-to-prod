package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZakariaAitAli/zero-to-prod/apps/work-items/internal/authn"
	"github.com/ZakariaAitAli/zero-to-prod/apps/work-items/internal/authz"
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

// grant builds a permission set the way the validator does, from a
// resource_access claim.
func grant(names ...string) authz.Set {
	roles := make([]any, len(names))
	for i, name := range names {
		roles[i] = name
	}
	set, _ := authz.Parse(map[string]any{authz.Client: map[string]any{"roles": roles}}, true)
	return set
}

func authenticatedHandler(validator tokenValidator) http.Handler {
	readiness := &readinessState{}
	readiness.set(true)
	return newApplicationHandler("test", readiness, &stubApplicationStore{}, requireBearerToken(validator))
}

func bearerRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer token-value")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

// The reviewed permission matrix, one request per business route.
var businessRoutes = []struct {
	method, path, body string
	permission         authz.Permission
}{
	{http.MethodGet, "/items", "", authz.ItemsRead},
	{http.MethodPost, "/items", `{"title":"x"}`, authz.ItemsCreate},
	{http.MethodPost, "/items/1/process", "", authz.ItemsProcess},
	{http.MethodGet, "/processing-jobs/1", "", authz.ItemsRead},
}

var allPermissions = []authz.Permission{authz.ItemsRead, authz.ItemsCreate, authz.ItemsProcess}

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

func TestAuthenticationRejectsInvalidTokenBeforeAuthorization(t *testing.T) {
	// A rejected token is 401 even if the (untrusted) token would carry roles.
	handler := authenticatedHandler(&fakeValidator{
		err:       &authn.Failure{Category: "expired"},
		principal: authn.Principal{Permissions: grant("items.create")},
	})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, bearerRequest(http.MethodPost, "/items", `{"title":"x"}`))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", recorder.Code)
	}
	if got := recorder.Header().Get("WWW-Authenticate"); !strings.Contains(got, `error="invalid_token"`) {
		t.Fatalf("unexpected WWW-Authenticate %q", got)
	}
}

func TestAuthenticationReportsUnavailableKeys(t *testing.T) {
	handler := authenticatedHandler(&fakeValidator{err: authn.ErrUnavailable})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, bearerRequest(http.MethodGet, "/items", ""))

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

func TestAuthenticationPassesValidTokenWithPermission(t *testing.T) {
	validator := &fakeValidator{principal: authn.Principal{Subject: "user-1", Permissions: grant("items.read")}}
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

func expectForbidden(t *testing.T, recorder *httptest.ResponseRecorder, label string) {
	t.Helper()
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("%s: expected 403, got %d", label, recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"insufficient_permissions"`) {
		t.Fatalf("%s: unexpected body %s", label, recorder.Body.String())
	}
	if got := recorder.Header().Get("WWW-Authenticate"); got != `Bearer realm="work-items", error="insufficient_scope"` {
		t.Fatalf("%s: unexpected WWW-Authenticate %q", label, got)
	}
}

func TestAuthorizationMatrix(t *testing.T) {
	for _, route := range businessRoutes {
		label := route.method + " " + route.path

		// No permissions: 403.
		recorder := httptest.NewRecorder()
		authenticatedHandler(&fakeValidator{principal: authn.Principal{Subject: "s"}}).
			ServeHTTP(recorder, bearerRequest(route.method, route.path, route.body))
		expectForbidden(t, recorder, label+" without permissions")

		// Every permission except the required one: 403.
		var others []string
		for _, p := range allPermissions {
			if p != route.permission {
				others = append(others, p.String())
			}
		}
		recorder = httptest.NewRecorder()
		authenticatedHandler(&fakeValidator{principal: authn.Principal{Subject: "s", Permissions: grant(others...)}}).
			ServeHTTP(recorder, bearerRequest(route.method, route.path, route.body))
		expectForbidden(t, recorder, label+" with only other permissions")

		// Only the required permission: the handler runs (any non-auth status).
		recorder = httptest.NewRecorder()
		authenticatedHandler(&fakeValidator{principal: authn.Principal{Subject: "s", Permissions: grant(route.permission.String())}}).
			ServeHTTP(recorder, bearerRequest(route.method, route.path, route.body))
		if recorder.Code == http.StatusForbidden || recorder.Code == http.StatusUnauthorized {
			t.Fatalf("%s with %s: expected the handler to run, got %d", label, route.permission, recorder.Code)
		}
	}
}

func TestAuthorizationDeniesMalformedRoleClaim(t *testing.T) {
	set, claim := authz.Parse(map[string]any{authz.Client: map[string]any{"roles": []any{"items.read", 1}}}, true)
	handler := authenticatedHandler(&fakeValidator{principal: authn.Principal{Subject: "s", Permissions: set, RoleClaim: claim}})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, bearerRequest(http.MethodGet, "/items", ""))
	expectForbidden(t, recorder, "malformed role claim")
}

func TestBusinessRouteWithoutPermissionPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("registering a business route without a permission must panic")
		}
	}()
	routes := protectedRoutes{mux: http.NewServeMux(), guard: withoutAuthentication}
	routes.Handle("GET /unprotected", authz.Permission{}, func(http.ResponseWriter, *http.Request) {})
}

func TestDisabledModeAppliesNoAuthorization(t *testing.T) {
	readiness := &readinessState{}
	readiness.set(true)
	handler := newHandler("test", readiness, &stubApplicationStore{})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/items", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("development mode: expected 200 without a token, got %d", recorder.Code)
	}
}
