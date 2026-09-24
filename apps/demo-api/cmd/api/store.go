package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type workItem struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type applicationStore interface {
	Ready(context.Context) error
	CreateWorkItem(context.Context, string, string) (workItem, error)
	ListWorkItems(context.Context) ([]workItem, error)
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
	const query = `
		SELECT id, title, status, created_at
		FROM public.work_items
		LIMIT 0
	`

	rows, err := store.pool.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("check PostgreSQL readiness: %w", err)
	}
	rows.Close()

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
