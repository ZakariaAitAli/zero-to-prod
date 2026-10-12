package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/ZakariaAitAli/zero-to-prod/apps/work-items/internal/authn"
)

// tokenValidator is satisfied by *authn.Validator.
type tokenValidator interface {
	Validate(context.Context, string) (authn.Principal, error)
}

type principalContextKey struct{}

// middleware wraps the business routes. Health, readiness, and version stay
// outside it.
type middleware func(http.Handler) http.Handler

// routes is where business handlers are registered.
type routes interface {
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}

// authenticatedRoutes registers each business route on the shared mux behind
// the authentication middleware, so method and path matching are unchanged.
type authenticatedRoutes struct {
	mux          *http.ServeMux
	authenticate middleware
}

func (r authenticatedRoutes) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	r.mux.Handle(pattern, r.authenticate(http.HandlerFunc(handler)))
}

// withoutAuthentication is used only when AUTHENTICATION=disabled.
func withoutAuthentication(next http.Handler) http.Handler {
	return next
}

// requireBearerToken rejects requests without a valid access token.
// Missing or rejected tokens get 401; tokens that cannot be judged because
// the issuer's keys are unavailable get 503.
func requireBearerToken(validator tokenValidator) middleware {
	return func(next http.Handler) http.Handler {
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
			case err == nil:
				ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
				next.ServeHTTP(w, r.WithContext(ctx))

			case errors.As(err, &failure):
				log.Printf("authentication rejected: category=%s", failure.Category)
				w.Header().Set("WWW-Authenticate", `Bearer realm="work-items", error="invalid_token"`)
				writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "unauthenticated"})

			default:
				log.Printf("authentication unavailable: %v", err)
				writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "authentication_unavailable"})
			}
		})
	}
}

// authenticationFromEnvironment applies the explicitly configured mode.
func authenticationFromEnvironment(ctx context.Context) (middleware, error) {
	config, err := authn.LoadConfig(os.Getenv)
	if err != nil {
		return nil, err
	}

	if config.Mode == authn.ModeDisabled {
		log.Printf("authentication disabled (AUTHENTICATION=disabled): development use only")
		return withoutAuthentication, nil
	}

	validator, err := authn.NewValidator(ctx, config)
	if err != nil {
		return nil, err
	}
	log.Printf("authentication required: issuer=%s audience=%s", config.Issuer, config.Audience)
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
