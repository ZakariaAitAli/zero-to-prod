package main

import (
	"context"
	"testing"
	"time"
)

func TestNewPostgresWorkerStoreRejectsInvalidConfiguration(
	t *testing.T,
) {
	store, err := newPostgresWorkerStore(
		context.Background(),
		"://invalid-postgresql-url",
	)

	if err == nil {
		if store != nil {
			store.Close()
		}

		t.Fatal(
			"invalid PostgreSQL configuration unexpectedly succeeded",
		)
	}

	if store != nil {
		store.Close()

		t.Fatal(
			"invalid PostgreSQL configuration returned a store",
		)
	}
}

func TestNewPostgresWorkerStoreDoesNotRequireDatabaseAvailability(
	t *testing.T,
) {
	// Port 1 is deliberately not our local PostgreSQL listener.
	// The constructor should parse/create the pool without requiring a
	// successful network connection, matching the API startup contract.
	databaseURL := "postgres://zero_to_prod_worker:unused@127.0.0.1:1/zero_to_prod?sslmode=disable"

	ctx, cancel := context.WithTimeout(
		context.Background(),
		250*time.Millisecond,
	)
	defer cancel()

	store, err := newPostgresWorkerStore(
		ctx,
		databaseURL,
	)
	if err != nil {
		t.Fatalf(
			"temporary PostgreSQL unavailability prevented store construction: %v",
			err,
		)
	}
	defer store.Close()

	if store.workerStore == nil {
		t.Fatal(
			"runtime store did not expose worker completion store",
		)
	}

	if store.pool == nil {
		t.Fatal(
			"runtime store did not own PostgreSQL pool",
		)
	}
}
