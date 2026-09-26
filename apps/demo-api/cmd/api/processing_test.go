package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAcceptWorkItemProcessing(t *testing.T) {
	createdAt := time.Date(
		2026,
		time.September,
		24,
		22,
		30,
		0,
		0,
		time.UTC,
	)

	store := &stubApplicationStore{
		acceptJob: processingJob{
			ID:           7,
			WorkItemID:   42,
			State:        "accepted",
			AttemptCount: 0,
			CreatedAt:    createdAt,
		},
	}

	readiness := &readinessState{}
	readiness.set(true)

	handler := newHandler(
		"test-version",
		readiness,
		store,
	)

	request := httptest.NewRequest(
		http.MethodPost,
		"/items/42/process",
		nil,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf(
			"expected status code %d, got %d",
			http.StatusAccepted,
			response.Code,
		)
	}

	if store.acceptWorkItemID != 42 {
		t.Fatalf(
			"expected store work item id 42, got %d",
			store.acceptWorkItemID,
		)
	}

	var job processingJob

	if err := json.NewDecoder(response.Body).Decode(&job); err != nil {
		t.Fatalf("failed to decode processing job: %v", err)
	}

	if job.ID != 7 {
		t.Fatalf("expected job id 7, got %d", job.ID)
	}

	if job.WorkItemID != 42 {
		t.Fatalf(
			"expected work_item_id 42, got %d",
			job.WorkItemID,
		)
	}

	if job.State != "accepted" {
		t.Fatalf(
			"expected state %q, got %q",
			"accepted",
			job.State,
		)
	}

	if job.AttemptCount != 0 {
		t.Fatalf(
			"expected attempt_count 0, got %d",
			job.AttemptCount,
		)
	}

	if !job.CreatedAt.Equal(createdAt) {
		t.Fatalf(
			"expected created_at %s, got %s",
			createdAt,
			job.CreatedAt,
		)
	}
}

func TestAcceptWorkItemProcessingRejectsInvalidItemID(t *testing.T) {
	store := &stubApplicationStore{}

	readiness := &readinessState{}
	readiness.set(true)

	handler := newHandler(
		"test-version",
		readiness,
		store,
	)

	request := httptest.NewRequest(
		http.MethodPost,
		"/items/not-a-number/process",
		nil,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf(
			"expected status code %d, got %d",
			http.StatusBadRequest,
			response.Code,
		)
	}

	if store.acceptWorkItemID != 0 {
		t.Fatalf(
			"store was called for invalid item id: %d",
			store.acceptWorkItemID,
		)
	}

	var body errorResponse

	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if body.Error != "invalid_item_id" {
		t.Fatalf(
			"expected invalid_item_id, got %q",
			body.Error,
		)
	}
}

func TestAcceptWorkItemProcessingReturnsNotFound(t *testing.T) {
	store := &stubApplicationStore{
		acceptErr: errWorkItemNotFound,
	}

	readiness := &readinessState{}
	readiness.set(true)

	handler := newHandler(
		"test-version",
		readiness,
		store,
	)

	request := httptest.NewRequest(
		http.MethodPost,
		"/items/404/process",
		nil,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf(
			"expected status code %d, got %d",
			http.StatusNotFound,
			response.Code,
		)
	}

	if store.acceptWorkItemID != 404 {
		t.Fatalf(
			"expected store work item id 404, got %d",
			store.acceptWorkItemID,
		)
	}

	var body errorResponse

	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if body.Error != "work_item_not_found" {
		t.Fatalf(
			"expected work_item_not_found, got %q",
			body.Error,
		)
	}
}

func TestAcceptWorkItemProcessingDoesNotExposeDatabaseError(t *testing.T) {
	store := &stubApplicationStore{
		acceptErr: errors.New(
			"password=secret host=private-database",
		),
	}

	readiness := &readinessState{}
	readiness.set(true)

	handler := newHandler(
		"test-version",
		readiness,
		store,
	)

	request := httptest.NewRequest(
		http.MethodPost,
		"/items/42/process",
		nil,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf(
			"expected status code %d, got %d",
			http.StatusServiceUnavailable,
			response.Code,
		)
	}

	body := response.Body.String()

	if strings.Contains(body, "secret") ||
		strings.Contains(body, "private-database") {
		t.Fatalf(
			"database details leaked in response: %s",
			body,
		)
	}

	if !strings.Contains(body, "persistence_unavailable") {
		t.Fatalf("unexpected response body: %s", body)
	}
}
