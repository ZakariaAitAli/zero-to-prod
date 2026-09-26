package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type workerPostgresIntegrationFixture struct {
	Store      *workerStore
	JobID      int64
	WorkItemID int64
}

func requireWorkerPostgresIntegrationFixture(
	t *testing.T,
	ctx context.Context,
) workerPostgresIntegrationFixture {
	t.Helper()

	workerDatabaseURL := os.Getenv("WORKER_DATABASE_URL")
	fixtureDatabaseURL := os.Getenv("WORKER_FIXTURE_DATABASE_URL")

	if workerDatabaseURL == "" ||
		fixtureDatabaseURL == "" {
		t.Skip(
			"WORKER_DATABASE_URL and WORKER_FIXTURE_DATABASE_URL are required",
		)
	}

	fixturePool, err := pgxpool.New(
		ctx,
		fixtureDatabaseURL,
	)
	if err != nil {
		t.Fatalf(
			"open worker integration fixture PostgreSQL pool: %v",
			err,
		)
	}

	var workItemID int64

	if err := fixturePool.QueryRow(
		ctx,
		`
			INSERT INTO public.work_items (title)
			VALUES ($1)
			RETURNING id
		`,
		"worker integration fixture: "+t.Name(),
	).Scan(
		&workItemID,
	); err != nil {
		fixturePool.Close()

		t.Fatalf(
			"create worker integration Work Item fixture: %v",
			err,
		)
	}

	var jobID int64

	if err := fixturePool.QueryRow(
		ctx,
		`
			INSERT INTO public.processing_jobs (work_item_id)
			VALUES ($1)
			RETURNING id
		`,
		workItemID,
	).Scan(
		&jobID,
	); err != nil {
		_, _ = fixturePool.Exec(
			ctx,
			`
				DELETE FROM public.work_items
				WHERE id = $1
			`,
			workItemID,
		)

		fixturePool.Close()

		t.Fatalf(
			"create worker integration processing-job fixture: %v",
			err,
		)
	}

	t.Cleanup(func() {
		cleanupContext, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()

		if _, err := fixturePool.Exec(
			cleanupContext,
			`
				DELETE FROM public.processing_jobs
				WHERE id = $1
			`,
			jobID,
		); err != nil {
			t.Errorf(
				"clean worker integration processing-job fixture: %v",
				err,
			)
		}

		if _, err := fixturePool.Exec(
			cleanupContext,
			`
				DELETE FROM public.work_items
				WHERE id = $1
			`,
			workItemID,
		); err != nil {
			t.Errorf(
				"clean worker integration Work Item fixture: %v",
				err,
			)
		}

		fixturePool.Close()
	})

	workerPool, err := pgxpool.New(
		ctx,
		workerDatabaseURL,
	)
	if err != nil {
		t.Fatalf(
			"open worker PostgreSQL pool: %v",
			err,
		)
	}

	t.Cleanup(func() {
		workerPool.Close()
	})

	return workerPostgresIntegrationFixture{
		Store: &workerStore{
			db: workerPool,
		},
		JobID:      jobID,
		WorkItemID: workItemID,
	}
}
