// redrive-after-restore explicitly returns delivery responsibility to the
// outbox for accepted jobs after an offline database restore. It never infers
// message loss from job age and never creates a new job.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ZakariaAitAli/zero-to-prod/apps/work-items/internal/runtimeconfig"
	"github.com/jackc/pgx/v5"
)

type candidate struct {
	JobID        int64  `json:"job_id"`
	WorkItemID   int64  `json:"work_item_id"`
	DeliveryID   string `json:"delivery_id"`
	WasPublished bool   `json:"was_published"`
	AttemptCount int    `json:"attempt_count"`
}

type recoveryReport struct {
	Database   string      `json:"database"`
	Applied    bool        `json:"applied"`
	Candidates []candidate `json:"accepted_jobs"`
}

func main() {
	apply := flag.Bool("after-restore", false, "redrive after an offline restore; otherwise inspect only")
	database := flag.String("database", "", "required database identity; must match the connected database")
	flag.Parse()
	if flag.NArg() != 0 || *database == "" {
		fmt.Fprintln(os.Stderr, "error: --database is required; use --after-restore only with API and workers stopped")
		os.Exit(2)
	}
	url, err := runtimeconfig.Value(os.Getenv, "RECOVERY_DATABASE_URL")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, url)
	// Connection errors can contain credential-bearing URLs. Keep diagnostics
	// generic; PostgreSQL logs retain the server-side reason.
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: cannot connect to recovery database")
		os.Exit(1)
	}
	defer conn.Close(context.Background())
	report, err := recoverAccepted(ctx, conn, *database, *apply)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, "error: cannot write recovery report")
		os.Exit(1)
	}
}

func recoverAccepted(ctx context.Context, conn *pgx.Conn, database string, apply bool) (recoveryReport, error) {
	report := recoveryReport{Candidates: []candidate{}}
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return report, errors.New("cannot begin recovery transaction")
	}
	defer tx.Rollback(context.Background())
	if err := tx.QueryRow(ctx, "SELECT current_database()").Scan(&report.Database); err != nil {
		return report, errors.New("cannot identify recovery database")
	}
	if report.Database != database {
		return report, errors.New("database identity does not match --database")
	}
	if apply {
		// Stop the processes, not just their broker consumers: an outbox publisher
		// holding a confirmed message could otherwise mark the redriven row later.
		var active int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND usename IN ('zero_to_prod_app','zero_to_prod_worker')`).Scan(&active); err != nil {
			return report, errors.New("cannot inspect runtime connections")
		}
		if active != 0 {
			return report, errors.New("stop every API and worker before recovery (runtime database connections remain)")
		}
		// Offline operation remains a runbook precondition. Locks also prevent an
		// unexpected writer from changing the inspected graph during this command.
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout='3s'; LOCK TABLE public.work_items, public.processing_jobs, public.outbox_messages, public.work_item_results IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			return report, errors.New("cannot lock recovery state; keep runtime stopped")
		}
	}
	var invalid int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM public.processing_jobs j
 LEFT JOIN public.work_items i ON i.id=j.work_item_id
 LEFT JOIN public.outbox_messages o ON o.processing_job_id=j.id
 LEFT JOIN public.work_item_results r ON r.work_item_id=j.work_item_id
 WHERE j.state='accepted' AND (i.status IS DISTINCT FROM 'pending' OR r.work_item_id IS NOT NULL
 OR o.id IS NULL OR o.payload->>'version' IS DISTINCT FROM '2'
 OR o.payload->>'type' IS DISTINCT FROM 'work_item.process'
 OR o.payload->>'job_id' IS DISTINCT FROM j.id::text
 OR o.payload->>'work_item_id' IS DISTINCT FROM j.work_item_id::text
 OR COALESCE(o.payload->>'delivery_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$')`).Scan(&invalid); err != nil {
		return report, errors.New("cannot validate restored job graph")
	}
	if invalid != 0 {
		return report, errors.New("accepted job graph or delivery identity is invalid; repair before redrive")
	}
	rows, err := tx.Query(ctx, `SELECT j.id,j.work_item_id,o.payload->>'delivery_id',o.published_at IS NOT NULL,j.attempt_count
 FROM public.processing_jobs j JOIN public.outbox_messages o ON o.processing_job_id=j.id
 WHERE j.state='accepted' ORDER BY j.id`)
	if err != nil {
		return report, errors.New("cannot inspect accepted jobs")
	}
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.JobID, &c.WorkItemID, &c.DeliveryID, &c.WasPublished, &c.AttemptCount); err != nil {
			rows.Close()
			return report, errors.New("cannot read accepted job")
		}
		report.Candidates = append(report.Candidates, c)
	}
	rows.Close()
	if rows.Err() != nil {
		return report, errors.New("cannot finish accepted-job inspection")
	}
	if apply {
		if _, err := tx.Exec(ctx, `UPDATE public.outbox_messages o SET published_at=NULL,last_error_code=NULL
 FROM public.processing_jobs j WHERE j.id=o.processing_job_id AND j.state='accepted'`); err != nil {
			return report, errors.New("cannot return delivery responsibility to outbox")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return report, errors.New("recovery commit unconfirmed; inspect before retrying")
	}
	report.Applied = apply
	return report, nil
}
