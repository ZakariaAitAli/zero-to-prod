package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStatusEndpoints(t *testing.T) {
	handler := newHandler("test-version")

	tests := []struct {
		name       string
		path       string
		wantStatus string
	}{
		{
			name:       "health endpoint",
			path:       "/health",
			wantStatus: "healthy",
		},
		{
			name:       "readiness endpoint",
			path:       "/ready",
			wantStatus: "ready",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf(
					"expected status code %d, got %d",
					http.StatusOK,
					response.Code,
				)
			}

			if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
				t.Fatalf(
					"expected Content-Type application/json, got %q",
					contentType,
				)
			}

			var body statusResponse

			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if body.Status != test.wantStatus {
				t.Errorf(
					"expected status %q, got %q",
					test.wantStatus,
					body.Status,
				)
			}
		})
	}
}

func TestVersionEndpoint(t *testing.T) {
	const expectedVersion = "abc123"

	handler := newHandler(expectedVersion)

	request := httptest.NewRequest(http.MethodGet, "/version", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected status code %d, got %d",
			http.StatusOK,
			response.Code,
		)
	}

	var body versionResponse

	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body.Version != expectedVersion {
		t.Errorf(
			"expected version %q, got %q",
			expectedVersion,
			body.Version,
		)
	}
}

func TestHealthEndpointRejectsPost(t *testing.T) {
	handler := newHandler("test-version")

	request := httptest.NewRequest(http.MethodPost, "/health", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Errorf(
			"expected status code %d, got %d",
			http.StatusMethodNotAllowed,
			response.Code,
		)
	}
}

func TestValidateRuntimeContract(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		observed string
		wantErr  bool
	}{
		{
			name:     "matching contract",
			expected: "A",
			observed: "A",
			wantErr:  false,
		},
		{
			name:     "mismatched contract",
			expected: "A",
			observed: "B",
			wantErr:  true,
		},
		{
			name:     "missing contract",
			expected: "A",
			observed: "",
			wantErr:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateRuntimeContract(test.expected, test.observed)

			if test.wantErr && err == nil {
				t.Fatal("expected runtime contract validation to fail")
			}

			if !test.wantErr && err != nil {
				t.Fatalf("expected runtime contract validation to succeed, got: %v", err)
			}
		})
	}
}

func TestHTTPServerConfiguration(t *testing.T) {
	server := newHTTPServer(
		"127.0.0.1:18081",
		newHandler("test-version"),
	)

	if server.Addr != "127.0.0.1:18081" {
		t.Errorf(
			"expected address %q, got %q",
			"127.0.0.1:18081",
			server.Addr,
		)
	}

	if server.Handler == nil {
		t.Fatal("expected HTTP handler to be configured")
	}

	tests := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{
			name: "read header timeout",
			got:  server.ReadHeaderTimeout,
			want: readHeaderTimeout,
		},
		{
			name: "read timeout",
			got:  server.ReadTimeout,
			want: readTimeout,
		},
		{
			name: "write timeout",
			got:  server.WriteTimeout,
			want: writeTimeout,
		},
		{
			name: "idle timeout",
			got:  server.IdleTimeout,
			want: idleTimeout,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Errorf(
					"expected %s %s, got %s",
					test.name,
					test.want,
					test.got,
				)
			}
		})
	}
}

func TestRunHTTPServerGracefulShutdown(t *testing.T) {
	shutdownContext, cancel := context.WithCancel(context.Background())
	cancel()

	server := newHTTPServer(
		"127.0.0.1:0",
		newHandler("test-version"),
	)

	if err := runHTTPServer(
		shutdownContext,
		server,
		time.Second,
	); err != nil {
		t.Fatalf(
			"expected graceful shutdown to succeed, got: %v",
			err,
		)
	}
}

func TestRunHTTPServerReturnsUnexpectedListenError(t *testing.T) {
	server := newHTTPServer(
		"127.0.0.1:99999",
		newHandler("test-version"),
	)

	err := runHTTPServer(
		context.Background(),
		server,
		time.Second,
	)

	if err == nil {
		t.Fatal("expected invalid listen address to fail")
	}

	if !strings.Contains(err.Error(), "serve HTTP") {
		t.Fatalf(
			"expected server error to identify HTTP serving failure, got: %v",
			err,
		)
	}
}
