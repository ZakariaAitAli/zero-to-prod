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

func readyTestHandler(appVersion string) http.Handler {
	readiness := &readinessState{}
	readiness.set(true)

	return newHandler(appVersion, readiness)
}

func TestStatusEndpoints(t *testing.T) {
	handler := readyTestHandler("test-version")

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

func TestReadinessEndpointTracksState(t *testing.T) {
	readiness := &readinessState{}
	handler := newHandler("test-version", readiness)

	assertReadiness := func(
		wantCode int,
		wantStatus string,
	) {
		t.Helper()

		request := httptest.NewRequest(http.MethodGet, "/ready", nil)
		response := httptest.NewRecorder()

		handler.ServeHTTP(response, request)

		if response.Code != wantCode {
			t.Fatalf(
				"expected status code %d, got %d",
				wantCode,
				response.Code,
			)
		}

		var body statusResponse

		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode readiness response: %v", err)
		}

		if body.Status != wantStatus {
			t.Fatalf(
				"expected readiness status %q, got %q",
				wantStatus,
				body.Status,
			)
		}
	}

	assertReadiness(
		http.StatusServiceUnavailable,
		"not_ready",
	)

	readiness.set(true)

	assertReadiness(
		http.StatusOK,
		"ready",
	)

	readiness.set(false)

	assertReadiness(
		http.StatusServiceUnavailable,
		"not_ready",
	)
}

func TestVersionEndpoint(t *testing.T) {
	const expectedVersion = "abc123"

	handler := readyTestHandler(expectedVersion)

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
	handler := readyTestHandler("test-version")

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

func TestHTTPServerConfiguration(t *testing.T) {
	server := newHTTPServer(
		"127.0.0.1:18081",
		readyTestHandler("test-version"),
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
	defer cancel()

	readiness := &readinessState{}

	server := newHTTPServer(
		"127.0.0.1:0",
		newHandler("test-version", readiness),
	)

	result := make(chan error, 1)

	go func() {
		result <- runHTTPServer(
			shutdownContext,
			server,
			readiness,
			time.Second,
		)
	}()

	deadline := time.Now().Add(time.Second)

	for !readiness.isReady() {
		if time.Now().After(deadline) {
			t.Fatal("server did not become ready after binding listener")
		}

		time.Sleep(10 * time.Millisecond)
	}

	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf(
				"expected graceful shutdown to succeed, got: %v",
				err,
			)
		}

	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for graceful shutdown")
	}

	if readiness.isReady() {
		t.Fatal("expected readiness to be false after shutdown")
	}
}

func TestRunHTTPServerReturnsUnexpectedListenError(t *testing.T) {
	readiness := &readinessState{}

	server := newHTTPServer(
		"127.0.0.1:99999",
		newHandler("test-version", readiness),
	)

	err := runHTTPServer(
		context.Background(),
		server,
		readiness,
		time.Second,
	)

	if err == nil {
		t.Fatal("expected invalid listen address to fail")
	}

	if !strings.Contains(err.Error(), "listen HTTP") {
		t.Fatalf(
			"expected server error to identify HTTP listen failure, got: %v",
			err,
		)
	}

	if readiness.isReady() {
		t.Fatal("failed listener binding must not mark server ready")
	}
}
