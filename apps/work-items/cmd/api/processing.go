package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
)

func registerProcessingHandlers(
	mux *http.ServeMux,
	store applicationStore,
) {
	mux.HandleFunc(
		"POST /items/{id}/process",
		func(w http.ResponseWriter, r *http.Request) {
			workItemID, err := strconv.ParseInt(
				r.PathValue("id"),
				10,
				64,
			)
			if err != nil || workItemID <= 0 {
				writeJSON(w, http.StatusBadRequest, errorResponse{
					Error: "invalid_item_id",
				})
				return
			}

			ctx, cancel := context.WithTimeout(
				r.Context(),
				databaseOperationTimeout,
			)
			defer cancel()

			job, err := store.AcceptProcessingJob(
				ctx,
				workItemID,
			)
			if errors.Is(err, errWorkItemNotFound) {
				writeJSON(w, http.StatusNotFound, errorResponse{
					Error: "work_item_not_found",
				})
				return
			}

			if err != nil {
				log.Printf(
					"accept processing job failed: %v",
					err,
				)

				writeJSON(
					w,
					http.StatusServiceUnavailable,
					errorResponse{
						Error: "persistence_unavailable",
					},
				)
				return
			}

			writeJSON(w, http.StatusAccepted, job)
		},
	)
}
