package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/ZakariaAitAli/zero-to-prod/apps/work-items/internal/authz"
)

func registerProcessingHandlers(
	mux routes,
	store applicationStore,
) {
	mux.Handle(
		"GET /processing-jobs/{id}",
		authz.ItemsRead,
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

	mux.Handle(
		"POST /items/{id}/process",
		authz.ItemsProcess,
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
			if errors.Is(err, errProcessingAlreadyActive) {
				writeJSON(w, http.StatusConflict, errorResponse{Error: "processing_already_active", ProcessingJobID: job.ID})
				return
			}
			if errors.Is(err, errWorkItemDone) {
				writeJSON(w, http.StatusConflict, errorResponse{Error: "work_item_already_done"})
				return
			}
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
