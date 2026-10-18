package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestOfflineRecoveryIntegration(t *testing.T) {
	raw := os.Getenv("RECOVERY_TEST_ADMIN_URL")
	if raw == "" {
		t.Skip("RECOVERY_TEST_ADMIN_URL required for isolated recovery integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, raw)
	if err != nil {
		t.Fatal("cannot connect to test admin")
	}
	defer admin.Close(context.Background())
	name := "recovery_test_" + time.Now().Format("150405000000000")
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	cfg, err := pgx.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database = name
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	files, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		sql, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("migration %s: %v", file, err)
		}
	}
	var jobID, failedID int64
	if err := conn.QueryRow(ctx, `WITH i AS (INSERT INTO public.work_items(title) VALUES ('recovery fixture') RETURNING id)
 INSERT INTO public.processing_jobs(work_item_id,attempt_count) SELECT id,2 FROM i RETURNING id`).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO public.outbox_messages(processing_job_id,event_type,payload,publish_attempts,published_at)
 SELECT id,'work_item.process',jsonb_build_object('type','work_item.process','version',2,'job_id',id,'work_item_id',work_item_id,'delivery_id',gen_random_uuid()::text),4,now() FROM public.processing_jobs WHERE id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `WITH i AS (INSERT INTO public.work_items(title) VALUES ('terminal fixture') RETURNING id)
 INSERT INTO public.processing_jobs(work_item_id,state,attempt_count,finished_at) SELECT id,'failed',3,now() FROM i RETURNING id`).Scan(&failedID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO public.outbox_messages(processing_job_id,event_type,payload,published_at)
 VALUES ($1,'work_item.process','{}',now())`, failedID); err != nil {
		t.Fatal(err)
	}
	if _, err := recoverAccepted(ctx, conn, "wrong_database", true); err == nil {
		t.Fatal("accepted wrong database identity")
	}
	report, err := recoverAccepted(ctx, conn, name, false)
	if err != nil || report.Applied || len(report.Candidates) != 1 || !report.Candidates[0].WasPublished || report.Candidates[0].AttemptCount != 2 {
		t.Fatalf("inspect: %+v %v", report, err)
	}
	identity := report.Candidates[0].DeliveryID
	// Even an idle runtime connection must block operator redrive.
	workerCfg, err := pgx.ParseConfig(os.Getenv("WORKER_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	workerCfg.Database = name
	worker, err := pgx.ConnectConfig(ctx, workerCfg)
	if err != nil {
		t.Fatal("cannot connect worker guard fixture")
	}
	if _, err := recoverAccepted(ctx, conn, name, true); err == nil || !strings.Contains(err.Error(), "runtime database connections remain") {
		t.Fatalf("runtime not refused: %v", err)
	}
	worker.Close(ctx)
	if _, err := conn.Exec(ctx, `UPDATE public.outbox_messages SET payload=payload||'{"version":1}' WHERE processing_job_id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	if _, err := recoverAccepted(ctx, conn, name, true); err == nil {
		t.Fatal("accepted invalid protocol in restored graph")
	}
	if _, err := conn.Exec(ctx, `UPDATE public.outbox_messages SET payload=payload||'{"version":2}' WHERE processing_job_id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		report, err = recoverAccepted(ctx, conn, name, true)
		if err != nil || !report.Applied || len(report.Candidates) != 1 || report.Candidates[0].JobID != jobID || report.Candidates[0].DeliveryID != identity {
			t.Fatalf("redrive: %+v %v", report, err)
		}
	}
	var published bool
	var attempts, publishAttempts, jobs int
	var afterIdentity string
	if err := conn.QueryRow(ctx, `SELECT o.published_at IS NOT NULL,j.attempt_count,o.publish_attempts,o.payload->>'delivery_id',(SELECT count(*) FROM public.processing_jobs)
 FROM public.processing_jobs j JOIN public.outbox_messages o ON o.processing_job_id=j.id WHERE j.id=$1`, jobID).Scan(&published, &attempts, &publishAttempts, &afterIdentity, &jobs); err != nil {
		t.Fatal(err)
	}
	if published || attempts != 2 || publishAttempts != 4 || afterIdentity != identity || jobs != 2 {
		t.Fatalf("redrive changed job/counters/identity: %t %d %d %s %d", published, attempts, publishAttempts, afterIdentity, jobs)
	}
	if err := conn.QueryRow(ctx, `SELECT published_at IS NOT NULL FROM public.outbox_messages WHERE processing_job_id=$1`, failedID).Scan(&published); err != nil || !published {
		t.Fatalf("terminal job redriven: %t %v", published, err)
	}
	// The offline upgrade/downgrade path must work with populated state.
	down, err := os.ReadFile("../../migrations/000006_add_delivery_identity.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	var version int
	if err := conn.QueryRow(ctx, `SELECT (payload->>'version')::int FROM public.outbox_messages WHERE processing_job_id=$1`, jobID).Scan(&version); err != nil || version != 1 {
		t.Fatalf("downgrade protocol: %d %v", version, err)
	}
	up, err := os.ReadFile("../../migrations/000006_add_delivery_identity.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	report, err = recoverAccepted(ctx, conn, name, false)
	if err != nil || len(report.Candidates) != 1 || report.Candidates[0].JobID != jobID || report.Candidates[0].DeliveryID == identity || report.Candidates[0].WasPublished {
		t.Fatalf("populated upgrade: %+v %v", report, err)
	}
	if err := conn.QueryRow(ctx, `SELECT published_at IS NOT NULL FROM public.outbox_messages WHERE processing_job_id=$1`, failedID).Scan(&published); err != nil || !published {
		t.Fatalf("upgrade redriven terminal job: %t %v", published, err)
	}

}
