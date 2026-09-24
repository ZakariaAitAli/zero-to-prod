package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type workItem struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type processingJob struct {
	ID           int64     `json:"id"`
	WorkItemID   int64     `json:"work_item_id"`
	State        string    `json:"state"`
	AttemptCount int       `json:"attempt_count"`
	CreatedAt    time.Time `json:"created_at"`
}

var errWorkItemNotFound = errors.New("work item not found")

type applicationStore interface {
	Ready(context.Context) error
	CreateWorkItem(context.Context, string, string) (workItem, error)
	ListWorkItems(context.Context) ([]workItem, error)
	AcceptProcessingJob(context.Context, int64) (processingJob, error)
}

type postgresStore struct {
	pool *pgxpool.Pool
}

func newPostgresStore(
	ctx context.Context,
	databaseURL string,
) (*postgresStore, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database configuration: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL pool: %w", err)
	}

	// Deliberately do not Ping here.
	//
	// Database availability is a runtime dependency/readiness concern.
	// A correctly configured API must be able to stay alive while
	// PostgreSQL is temporarily unavailable.
	return &postgresStore{
		pool: pool,
	}, nil
}

func (store *postgresStore) Ready(ctx context.Context) error {
	queries := []string{
		`
			SELECT id, title, status, created_at
			FROM public.work_items
			LIMIT 0
		`,
		`
			SELECT
				id,
				work_item_id,
				state,
				attempt_count,
				last_error_code,
				created_at,
				finished_at
			FROM public.processing_jobs
			LIMIT 0
		`,
		`
			SELECT
				id,
				processing_job_id,
				event_type,
				payload,
				publish_attempts,
				last_error_code,
				created_at,
				published_at
			FROM public.outbox_messages
			LIMIT 0
		`,
	}

	for _, query := range queries {
		rows, err := store.pool.Query(ctx, query)
		if err != nil {
			return fmt.Errorf("check PostgreSQL readiness: %w", err)
		}

		rows.Close()
	}

	return nil
}

func (store *postgresStore) CreateWorkItem(
	ctx context.Context,
	title string,
	status string,
) (workItem, error) {
	const query = `
		INSERT INTO public.work_items (title, status)
		VALUES ($1, $2)
		RETURNING id, title, status, created_at
	`

	var item workItem

	err := store.pool.QueryRow(
		ctx,
		query,
		title,
		status,
	).Scan(
		&item.ID,
		&item.Title,
		&item.Status,
		&item.CreatedAt,
	)
	if err != nil {
		return workItem{}, fmt.Errorf("insert work item: %w", err)
	}

	return item, nil
}

func (store *postgresStore) AcceptProcessingJob(
	ctx context.Context,
	workItemID int64,
) (processingJob, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return processingJob{}, fmt.Errorf(
			"begin processing acceptance transaction: %w",
			err,
		)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	const insertJobQuery = `
		INSERT INTO public.processing_jobs (work_item_id)
		SELECT id
		FROM public.work_items
		WHERE id = $1
		RETURNING
			id,
			work_item_id,
			state,
			attempt_count,
			created_at
	`

	var job processingJob

	err = tx.QueryRow(
		ctx,
		insertJobQuery,
		workItemID,
	).Scan(
		&job.ID,
		&job.WorkItemID,
		&job.State,
		&job.AttemptCount,
		&job.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return processingJob{}, errWorkItemNotFound
	}
	if err != nil {
		return processingJob{}, fmt.Errorf(
			"insert processing job: %w",
			err,
		)
	}

	const insertOutboxQuery = `
		INSERT INTO public.outbox_messages (
			processing_job_id,
			event_type,
			payload
		)
		VALUES (
			$1::BIGINT,
			'work_item.process',
			jsonb_build_object(
				'type', 'work_item.process',
				'version', 1,
				'job_id', $1::BIGINT,
				'work_item_id', $2::BIGINT
			)
		)
	`

	if _, err := tx.Exec(
		ctx,
		insertOutboxQuery,
		job.ID,
		job.WorkItemID,
	); err != nil {
		return processingJob{}, fmt.Errorf(
			"insert processing outbox message: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return processingJob{}, fmt.Errorf(
			"commit processing acceptance transaction: %w",
			err,
		)
	}

	return job, nil
}

func (store *postgresStore) ListWorkItems(
	ctx context.Context,
) ([]workItem, error) {
	const query = `
		SELECT id, title, status, created_at
		FROM public.work_items
		ORDER BY id
	`

	rows, err := store.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query work items: %w", err)
	}
	defer rows.Close()

	items := make([]workItem, 0)

	for rows.Next() {
		var item workItem

		if err := rows.Scan(
			&item.ID,
			&item.Title,
			&item.Status,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan work item: %w", err)
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate work items: %w", err)
	}

	return items, nil
}

func (store *postgresStore) Close() {
	store.pool.Close()
}
