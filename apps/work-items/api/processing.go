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
		"GET /processing-jobs/{id}",
		func(w http.ResponseWriter, r *http.Request) {
			jobID, err := strconv.ParseInt(
				r.PathValue("id"),
				10,
				64,
			)
			if err != nil || jobID <= 0 {
				writeJSON(w, http.StatusBadRequest, errorResponse{
					Error: "invalid_processing_job_id",
				})
				return
			}

			ctx, cancel := context.WithTimeout(
				r.Context(),
				databaseOperationTimeout,
			)
			defer cancel()

			job, err := store.GetProcessingJob(ctx, jobID)
			if errors.Is(err, errProcessingJobNotFound) {
				writeJSON(w, http.StatusNotFound, errorResponse{
					Error: "processing_job_not_found",
				})
				return
			}

			if err != nil {
				log.Printf(
					"get processing job failed: %v",
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

			writeJSON(w, http.StatusOK, job)
		},
	)

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
