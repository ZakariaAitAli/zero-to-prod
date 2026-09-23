package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

const databaseOperationTimeout = 3 * time.Second

type createWorkItemRequest struct {
	Title string `json:"title"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func registerWorkItemHandlers(
	mux *http.ServeMux,
	store applicationStore,
) {
	mux.HandleFunc("POST /items", func(w http.ResponseWriter, r *http.Request) {
		var request createWorkItemRequest

		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()

		if err := decoder.Decode(&request); err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{
				Error: "invalid_request",
			})
			return
		}

		request.Title = strings.TrimSpace(request.Title)
		if request.Title == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse{
				Error: "title_required",
			})
			return
		}

		ctx, cancel := context.WithTimeout(
			r.Context(),
			databaseOperationTimeout,
		)
		defer cancel()

		item, err := store.CreateWorkItem(ctx, request.Title)
		if err != nil {
			log.Printf("create work item failed: %v", err)

			writeJSON(w, http.StatusServiceUnavailable, errorResponse{
				Error: "persistence_unavailable",
			})
			return
		}

		writeJSON(w, http.StatusCreated, item)
	})

	mux.HandleFunc("GET /items", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(
			r.Context(),
			databaseOperationTimeout,
		)
		defer cancel()

		items, err := store.ListWorkItems(ctx)
		if err != nil {
			log.Printf("list work items failed: %v", err)

			writeJSON(w, http.StatusServiceUnavailable, errorResponse{
				Error: "persistence_unavailable",
			})
			return
		}

		writeJSON(w, http.StatusOK, items)
	})
}
