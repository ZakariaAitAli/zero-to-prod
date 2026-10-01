package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresWorkerStore struct {
	*workerStore
	pool *pgxpool.Pool
}

func newPostgresWorkerStore(
	ctx context.Context,
	databaseURL string,
) (*postgresWorkerStore, error) {
	config, err := pgxpool.ParseConfig(
		databaseURL,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"parse worker PostgreSQL configuration: %w",
			err,
		)
	}

	pool, err := pgxpool.NewWithConfig(
		ctx,
		config,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create worker PostgreSQL pool: %w",
			err,
		)
	}

	// Deliberately do not Ping here.
	//
	// A valid worker configuration should be able to start while PostgreSQL
	// is temporarily unavailable. Runtime database failures are handled by
	// message settlement and the worker session retry lifecycle.
	return &postgresWorkerStore{
		workerStore: &workerStore{
			db: pool,
		},
		pool: pool,
	}, nil
}

func (store *postgresWorkerStore) Close() {
	if store.pool != nil {
		store.pool.Close()
	}
}
