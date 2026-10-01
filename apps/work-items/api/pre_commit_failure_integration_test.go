package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	acceptanceFailureTriggerName = "issue103_reject_test_outbox_insert"

	acceptanceFailureFunctionName = "issue103_reject_test_outbox_insert"
)

func TestProcessingAcceptanceRollsBackWhenOutboxInsertFails(
	t *testing.T,
) {
	appDatabaseURL := os.Getenv(
		"OUTBOX_INTEGRATION_DATABASE_URL",
	)
	migratorDatabaseURL := os.Getenv(
		"ACCEPTANCE_FAILURE_MIGRATOR_DATABASE_URL",
	)

	if appDatabaseURL == "" ||
		migratorDatabaseURL == "" {
		t.Skip(
			"OUTBOX_INTEGRATION_DATABASE_URL and ACCEPTANCE_FAILURE_MIGRATOR_DATABASE_URL are required",
		)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
	defer cancel()

	migratorPool, err := pgxpool.New(
		ctx,
		migratorDatabaseURL,
	)
	if err != nil {
		t.Fatalf(
			"open migrator PostgreSQL pool: %v",
			err,
		)
	}
	dropFailureInjection := func(
		cleanupContext context.Context,
	) error {
		if _, err := migratorPool.Exec(
			cleanupContext,
			fmt.Sprintf(
				`DROP TRIGGER IF EXISTS %s
				 ON public.outbox_messages`,
				acceptanceFailureTriggerName,
			),
		); err != nil {
			return fmt.Errorf(
				"drop failure-injection trigger: %w",
				err,
			)
		}

		if _, err := migratorPool.Exec(
			cleanupContext,
			fmt.Sprintf(
				`DROP FUNCTION IF EXISTS public.%s()`,
				acceptanceFailureFunctionName,
			),
		); err != nil {
			return fmt.Errorf(
				"drop failure-injection function: %w",
				err,
			)
		}

		return nil
	}

	// Remove any stale objects left by an interrupted previous test.
	if err := dropFailureInjection(ctx); err != nil {
		t.Fatalf(
			"clean stale failure injection: %v",
			err,
		)
	}

	t.Cleanup(func() {
		cleanupContext, cleanupCancel :=
			context.WithTimeout(
				context.Background(),
				5*time.Second,
			)
		defer cleanupCancel()

		if err := dropFailureInjection(
			cleanupContext,
		); err != nil {
			t.Errorf(
				"clean failure injection: %v",
				err,
			)
		}

		migratorPool.Close()
	})

	appStore, err := newPostgresStore(
		ctx,
		appDatabaseURL,
	)
	if err != nil {
		t.Fatalf(
			"open API PostgreSQL store: %v",
			err,
		)
	}
	defer appStore.Close()

	item, err := appStore.CreateWorkItem(
		ctx,
		"issue-103-pre-commit-failure",
		workItemStatusPending,
	)
	if err != nil {
		t.Fatalf(
			"create failure-test Work Item: %v",
			err,
		)
	}

	createFunctionSQL := fmt.Sprintf(`
		CREATE FUNCTION public.%s()
		RETURNS trigger
		LANGUAGE plpgsql
		AS $$
		DECLARE
			target_title TEXT;
		BEGIN
			SELECT wi.title
			INTO target_title
			FROM public.processing_jobs AS pj
			JOIN public.work_items AS wi
			  ON wi.id = pj.work_item_id
			WHERE pj.id = NEW.processing_job_id;

			IF target_title = 'issue-103-pre-commit-failure' THEN
				RAISE EXCEPTION
					'issue103 forced pre-commit acceptance failure'
					USING ERRCODE = 'P0001';
			END IF;

			RETURN NEW;
		END;
		$$
	`, acceptanceFailureFunctionName)

	if _, err := migratorPool.Exec(
		ctx,
		createFunctionSQL,
	); err != nil {
		t.Fatalf(
			"create failure-injection function: %v",
			err,
		)
	}

	createTriggerSQL := fmt.Sprintf(`
		CREATE TRIGGER %s
		BEFORE INSERT
		ON public.outbox_messages
		FOR EACH ROW
		EXECUTE FUNCTION public.%s()
	`,
		acceptanceFailureTriggerName,
		acceptanceFailureFunctionName,
	)

	if _, err := migratorPool.Exec(
		ctx,
		createTriggerSQL,
	); err != nil {
		t.Fatalf(
			"create failure-injection trigger: %v",
			err,
		)
	}

	readiness := &readinessState{}
	readiness.set(true)

	handler := newHandler(
		"issue-103-pre-commit-integration",
		readiness,
		appStore,
	)

	request := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf(
			"/items/%d/process",
			item.ID,
		),
		nil,
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(
		response,
		request,
	)

	if response.Code == http.StatusAccepted {
		t.Fatalf(
			"failed transaction was falsely accepted: status=%d body=%s",
			response.Code,
			response.Body.String(),
		)
	}

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf(
			"expected HTTP %d, got %d body=%s",
			http.StatusServiceUnavailable,
			response.Code,
			response.Body.String(),
		)
	}

	var errorBody errorResponse

	if err := json.NewDecoder(
		response.Body,
	).Decode(
		&errorBody,
	); err != nil {
		t.Fatalf(
			"decode failure response: %v",
			err,
		)
	}

	if errorBody.Error != "persistence_unavailable" {
		t.Fatalf(
			"expected persistence_unavailable, got %q",
			errorBody.Error,
		)
	}

	var (
		jobCount       int
		outboxCount    int
		workItemStatus string
	)

	err = appStore.pool.QueryRow(
		ctx,
		`
			SELECT
				(
					SELECT COUNT(*)
					FROM public.processing_jobs
					WHERE work_item_id = $1
				),
				(
					SELECT COUNT(*)
					FROM public.outbox_messages AS om
					JOIN public.processing_jobs AS pj
					  ON pj.id = om.processing_job_id
					WHERE pj.work_item_id = $1
				),
				(
					SELECT status
					FROM public.work_items
					WHERE id = $1
				)
		`,
		item.ID,
	).Scan(
		&jobCount,
		&outboxCount,
		&workItemStatus,
	)
	if err != nil {
		t.Fatalf(
			"inspect rollback result: %v",
			err,
		)
	}

	if jobCount != 0 {
		t.Fatalf(
			"failed transaction left %d phantom processing jobs",
			jobCount,
		)
	}

	if outboxCount != 0 {
		t.Fatalf(
			"failed transaction left %d phantom outbox rows",
			outboxCount,
		)
	}

	if workItemStatus != workItemStatusPending {
		t.Fatalf(
			"failed acceptance mutated Work Item status to %q",
			workItemStatus,
		)
	}

	t.Logf(
		"work_item_id=%d response=%d processing_jobs=%d outbox_messages=%d status=%s",
		item.ID,
		response.Code,
		jobCount,
		outboxCount,
		workItemStatus,
	)
}
