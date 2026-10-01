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

func TestCreateWorkItem(t *testing.T) {
	createdAt := time.Date(
		2026,
		time.September,
		23,
		19,
		0,
		0,
		0,
		time.UTC,
	)

	store := &stubApplicationStore{
		createItem: workItem{
			ID:        42,
			Title:     "ship persistence",
			CreatedAt: createdAt,
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
		"/items",
		strings.NewReader(`{"title":"ship persistence"}`),
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf(
			"expected status code %d, got %d",
			http.StatusCreated,
			response.Code,
		)
	}

	var item workItem

	if err := json.NewDecoder(response.Body).Decode(&item); err != nil {
		t.Fatalf("failed to decode created work item: %v", err)
	}

	if item.ID != 42 {
		t.Fatalf("expected id 42, got %d", item.ID)
	}

	if item.Title != "ship persistence" {
		t.Fatalf(
			"expected title %q, got %q",
			"ship persistence",
			item.Title,
		)
	}

	if !item.CreatedAt.Equal(createdAt) {
		t.Fatalf(
			"expected created_at %s, got %s",
			createdAt,
			item.CreatedAt,
		)
	}
}

func TestCreateWorkItemRejectsBlankTitle(t *testing.T) {
	readiness := &readinessState{}
	readiness.set(true)

	handler := newHandler(
		"test-version",
		readiness,
		&stubApplicationStore{},
	)

	request := httptest.NewRequest(
		http.MethodPost,
		"/items",
		strings.NewReader(`{"title":"   "}`),
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
}

func TestCreateWorkItemDoesNotExposeDatabaseError(t *testing.T) {
	readiness := &readinessState{}
	readiness.set(true)

	handler := newHandler(
		"test-version",
		readiness,
		&stubApplicationStore{
			createErr: errors.New(
				"password=secret host=private-database",
			),
		},
	)

	request := httptest.NewRequest(
		http.MethodPost,
		"/items",
		strings.NewReader(`{"title":"failure test"}`),
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
		t.Fatalf("database details leaked in response: %s", body)
	}

	if !strings.Contains(body, "persistence_unavailable") {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestListWorkItems(t *testing.T) {
	createdAt := time.Date(
		2026,
		time.September,
		23,
		19,
		0,
		0,
		0,
		time.UTC,
	)

	store := &stubApplicationStore{
		listItems: []workItem{
			{
				ID:        1,
				Title:     "first",
				CreatedAt: createdAt,
			},
			{
				ID:        2,
				Title:     "second",
				CreatedAt: createdAt.Add(time.Minute),
			},
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
		http.MethodGet,
		"/items",
		nil,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected status code %d, got %d",
			http.StatusOK,
			response.Code,
		)
	}

	var items []workItem

	if err := json.NewDecoder(response.Body).Decode(&items); err != nil {
		t.Fatalf("failed to decode work items: %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	if items[0].ID != 1 || items[1].ID != 2 {
		t.Fatalf("unexpected item order: %#v", items)
	}
}

func TestListWorkItemsReturnsEmptyArray(t *testing.T) {
	readiness := &readinessState{}
	readiness.set(true)

	handler := newHandler(
		"test-version",
		readiness,
		&stubApplicationStore{
			listItems: []workItem{},
		},
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/items",
		nil,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected status code %d, got %d",
			http.StatusOK,
			response.Code,
		)
	}

	if body := response.Body.String(); body != "[]\n" {
		t.Fatalf("expected empty JSON array, got %q", body)
	}
}

func TestCreateWorkItemDefaultsStatusToPending(t *testing.T) {
	store := &stubApplicationStore{
		createItem: workItem{
			ID:     100,
			Title:  "default status",
			Status: workItemStatusPending,
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
		"/items",
		strings.NewReader(`{"title":"default status"}`),
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf(
			"expected status code %d, got %d",
			http.StatusCreated,
			response.Code,
		)
	}

	if store.createStatus != workItemStatusPending {
		t.Fatalf(
			"expected default status %q, got %q",
			workItemStatusPending,
			store.createStatus,
		)
	}
}

func TestCreateWorkItemAcceptsDoneStatus(t *testing.T) {
	store := &stubApplicationStore{
		createItem: workItem{
			ID:     101,
			Title:  "completed item",
			Status: workItemStatusDone,
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
		"/items",
		strings.NewReader(`{"title":"completed item","status":"done"}`),
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf(
			"expected status code %d, got %d",
			http.StatusCreated,
			response.Code,
		)
	}

	if store.createStatus != workItemStatusDone {
		t.Fatalf(
			"expected status %q, got %q",
			workItemStatusDone,
			store.createStatus,
		)
	}
}

func TestCreateWorkItemRejectsUnknownStatus(t *testing.T) {
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
		"/items",
		strings.NewReader(`{"title":"invalid status","status":"blocked"}`),
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

	if store.createTitle != "" || store.createStatus != "" {
		t.Fatalf(
			"store was called for invalid status: title=%q status=%q",
			store.createTitle,
			store.createStatus,
		)
	}

	var responseBody errorResponse
	if err := json.NewDecoder(response.Body).Decode(&responseBody); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if responseBody.Error != "invalid_status" {
		t.Fatalf(
			"expected invalid_status, got %q",
			responseBody.Error,
		)
	}
}
