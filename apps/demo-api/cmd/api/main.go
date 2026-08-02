package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

// version is replaced at build time using:
//
//	go build -ldflags="-X main.version=<commit-sha>"
var version = "dev"

type statusResponse struct {
	Status string `json:"status"`
}

type versionResponse struct {
	Version string `json:"version"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	address := ":" + port

	log.Printf("demo-api version=%s listening on %s", version, address)

	if err := http.ListenAndServe(address, newHandler(version)); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}

func newHandler(appVersion string) http.Handler {
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
