package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

// version is replaced at build time using:
//
//	go build -ldflags="-X main.version=<commit-sha>"
var version = "dev"

const (
	expectedRuntimeContract = "B"

	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 10 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
)

type statusResponse struct {
	Status string `json:"status"`
}

type versionResponse struct {
	Version string `json:"version"`
}

type readinessState struct {
	ready atomic.Bool
}

func (state *readinessState) set(ready bool) {
	state.ready.Store(ready)
}

func (state *readinessState) isReady() bool {
	return state.ready.Load()
}

func main() {
	runtimeContract := os.Getenv("RUNTIME_CONTRACT")

	if err := validateRuntimeContract(expectedRuntimeContract, runtimeContract); err != nil {
		log.Fatalf("invalid runtime configuration: %v", err)
	}

	log.Printf(
		"runtime contract verified: expected=%q observed=%q",
		expectedRuntimeContract,
		runtimeContract,
	)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	address := ":" + port
	readiness := &readinessState{}
	server := newHTTPServer(
		address,
		newHandler(version, readiness),
	)

	shutdownContext, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	log.Printf("demo-api version=%s listening on %s", version, address)

	if err := runHTTPServer(
		shutdownContext,
		server,
		readiness,
		shutdownTimeout,
	); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}

func newHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

func runHTTPServer(
	shutdownContext context.Context,
	server *http.Server,
	readiness *readinessState,
	gracePeriod time.Duration,
) error {
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}
	defer listener.Close()

	readiness.set(true)
	log.Printf("readiness changed: ready")

	serverErrors := make(chan error, 1)

	go func() {
		serverErrors <- server.Serve(listener)
	}()

	select {
	case err := <-serverErrors:
		readiness.set(false)

		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return fmt.Errorf("serve HTTP: %w", err)

	case <-shutdownContext.Done():
		readiness.set(false)
		log.Printf("readiness changed: not ready")
		log.Printf("shutdown requested")

		gracefulContext, cancel := context.WithTimeout(
			context.Background(),
			gracePeriod,
		)
		defer cancel()

		if err := server.Shutdown(gracefulContext); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}

		err := <-serverErrors

		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP after shutdown: %w", err)
		}

		log.Printf("graceful shutdown complete")

		return nil
	}
}

func validateRuntimeContract(expected, observed string) error {
	if observed == "" {
		return fmt.Errorf("RUNTIME_CONTRACT is required; expected %q", expected)
	}

	if observed != expected {
		return fmt.Errorf(
			"RUNTIME_CONTRACT mismatch: expected %q, got %q",
			expected,
			observed,
		)
	}

	return nil
}

func newHandler(
	appVersion string,
	readiness *readinessState,
) http.Handler {
	if appVersion == "" {
		appVersion = "dev"
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, statusResponse{
			Status: "healthy",
		})
	})

	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, _ *http.Request) {
		if !readiness.isReady() {
			writeJSON(w, http.StatusServiceUnavailable, statusResponse{
				Status: "not_ready",
			})
			return
		}

		writeJSON(w, http.StatusOK, statusResponse{
			Status: "ready",
		})
	})

	mux.HandleFunc("GET /version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, versionResponse{
			Version: appVersion,
		})
	})

	return mux
}

func writeJSON(w http.ResponseWriter, statusCode int, response any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("failed to encode JSON response: %v", err)
	}
}
