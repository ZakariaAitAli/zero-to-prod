package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/ZakariaAitAli/zero-to-prod/apps/work-items/internal/authn"
	"github.com/ZakariaAitAli/zero-to-prod/apps/work-items/internal/authz"
)

// tokenValidator is satisfied by *authn.Validator.
type tokenValidator interface {
	Validate(context.Context, string) (authn.Principal, error)
}

type principalContextKey struct{}

// guard protects one business route: it authenticates the request and then
// checks the route's permission. Health, readiness, and version stay outside.
type guard func(pattern string, permission authz.Permission, next http.Handler) http.Handler

// routes is the only way business handlers are registered: every route names
// the permission it requires, and there is no registration method without
// one. Business registration functions receive this interface, not the mux.
type routes interface {
	Handle(pattern string, permission authz.Permission, handler http.HandlerFunc)
}

// protectedRoutes registers each business route on the shared mux behind the
// guard, so method and path matching are unchanged.
type protectedRoutes struct {
	mux   *http.ServeMux
	guard guard
}

func (r protectedRoutes) Handle(pattern string, permission authz.Permission, handler http.HandlerFunc) {
	if !permission.Valid() {
		panic(fmt.Sprintf("business route %q registered without a permission", pattern))
	}
	r.mux.Handle(pattern, r.guard(pattern, permission, handler))
}

// withoutAuthentication is used only when AUTHENTICATION=disabled: neither
// authentication nor authorization applies.
func withoutAuthentication(_ string, _ authz.Permission, next http.Handler) http.Handler {
	return next
}

// requireBearerToken authenticates with the validator, then authorizes:
//   - missing or rejected token: 401 with a Bearer challenge;
//   - signing keys unavailable: 503;
//   - valid token without the route's permission: 403.
func requireBearerToken(validator tokenValidator) guard {
	return func(pattern string, permission authz.Permission, next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				w.Header().Set("WWW-Authenticate", `Bearer realm="work-items"`)
				writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "unauthenticated"})
				return
			}

			principal, err := validator.Validate(r.Context(), token)
			var failure *authn.Failure
			switch {
			case errors.As(err, &failure):
				log.Printf("authentication rejected: category=%s", failure.Category)
				w.Header().Set("WWW-Authenticate", `Bearer realm="work-items", error="invalid_token"`)
				writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "unauthenticated"})
				return
			case err != nil:
				log.Printf("authentication unavailable: %v", err)
				writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "authentication_unavailable"})
				return
			}

			if principal.RoleClaim == authz.ClaimMalformed {
				log.Printf("authorization role claim malformed: subject=%s azp=%s", principal.Subject, principal.AuthorizedParty)
			}
			if !principal.Permissions.Has(permission) {
				log.Printf("authorization denied: subject=%s azp=%s permission=%s route=%q",
					principal.Subject, principal.AuthorizedParty, permission, pattern)
				w.Header().Set("WWW-Authenticate", `Bearer realm="work-items", error="insufficient_scope"`)
				writeJSON(w, http.StatusForbidden, errorResponse{Error: "insufficient_permissions"})
				return
			}

			ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// authenticationFromEnvironment applies the explicitly configured mode.
func authenticationFromEnvironment(ctx context.Context) (guard, error) {
	config, err := authn.LoadConfig(os.Getenv)
	if err != nil {
		return nil, err
	}

	if config.Mode == authn.ModeDisabled {
		log.Printf("authentication and authorization disabled (AUTHENTICATION=disabled): development use only")
		return withoutAuthentication, nil
	}

	validator, err := authn.NewValidator(ctx, config)
	if err != nil {
		return nil, err
	}
	log.Printf("authentication and authorization required: issuer=%s audience=%s permissions from resource_access[%q].roles",
		config.Issuer, config.Audience, authz.Client)
	return requireBearerToken(validator), nil
}

func bearerToken(header string) (string, bool) {
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}
