//go:build crashexperiment

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type sequenceState struct {
	LastValue int64 `json:"last_value"`
	IsCalled  bool  `json:"is_called"`
}

func readSequences(t *testing.T, lab *experimentLab) map[string]sequenceState {
	t.Helper()
	sequences := map[string]sequenceState{}
	for _, name := range []string{"work_items_id_seq", "processing_jobs_id_seq", "outbox_messages_id_seq"} {
		var state sequenceState
		if err := lab.admin.QueryRow(context.Background(), "SELECT last_value, is_called FROM public."+name).
			Scan(&state.LastValue, &state.IsCalled); err != nil {
			t.Fatal(err)
		}
		sequences[name] = state
	}
	return sequences
}

func (lab *experimentLab) tool(t *testing.T, script string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	output, err := command(ctx, lab.config.RepoRoot, []string{
		"ZTP_COMPOSE_PROJECT_NAME=" + lab.config.PostgresProject,
		"ZTP_POSTGRES_PORT=" + lab.config.PostgresPort,
	}, filepath.Join(lab.config.RepoRoot, "tools", script), args...)
	if err != nil {
		t.Fatalf("%s %s: %v", script, strings.Join(args, " "), err)
	}
	return output
}

func (lab *experimentLab) reopenPools(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	var err error
	if lab.admin, err = pgxpool.New(ctx, lab.config.AdminURL); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lab.admin.Close)
	if lab.fixture, err = pgxpool.New(ctx, lab.config.FixtureURL); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lab.fixture.Close)
}

// destroyAndRestore destroys only the validated isolated PostgreSQL project,
// provisions it migration-first, and restores the logical backup.
func (lab *experimentLab) destroyAndRestore(t *testing.T, backupPath string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	volume := lab.config.PostgresProject + "_postgres_data"
	volumeProject, err := command(ctx, "", nil, "docker", "volume", "inspect", "--format",
		`{{index .Labels "com.docker.compose.project"}}`, volume)
	if err != nil || volumeProject != lab.config.PostgresProject {
		t.Fatalf("refusing to destroy volume %s labelled %q: %v", volume, volumeProject, err)
	}
	mounts, err := command(ctx, "", nil, "docker", "inspect", "--format", "{{range .Mounts}}{{.Name}} {{end}}", lab.config.PostgresContainer)
	if err != nil || !slices.Contains(strings.Fields(mounts), volume) {
		t.Fatalf("validated container does not mount %s: %q %v", volume, mounts, err)
	}
	if _, err := composeContainer(ctx, lab.config.PostgresProject, "postgres", "5432/tcp", lab.config.PostgresPort); err != nil {
		t.Fatalf("refusing to destroy unverified project: %v", err)
	}
	lab.record.sync("validate destroy target", "volume label, container mount, port binding and project prefix verified",
		map[string]string{"volume": volume, "project": lab.config.PostgresProject})

	lab.admin.Close()
	lab.fixture.Close()
	lab.tool(t, "postgres-local", "destroy")
	if _, err := command(ctx, "", nil, "docker", "volume", "inspect", volume); err == nil {
		t.Fatalf("volume %s still exists after destroy", volume)
	}
	present, err := presentProtectedVolumes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lab.record.check("protected lab volumes untouched by destroy", lab.config.PresentProtected, present,
		slices.Equal(present, lab.config.PresentProtected))
	lab.record.sync("isolated PostgreSQL destroyed", "volume "+volume+" absent", nil)

	lab.tool(t, "postgres-local", "start")
	lab.tool(t, "postgres-local", "migrate-up")
	container, err := composeContainer(ctx, lab.config.PostgresProject, "postgres", "5432/tcp", lab.config.PostgresPort)
	if err != nil {
		t.Fatal(err)
	}
	lab.config.PostgresContainer = container
	experimentShared.PostgresContainer = container
	lab.reopenPools(t)
	var identifier string
	if err := lab.admin.QueryRow(ctx, "SELECT system_identifier::text FROM pg_control_system()").Scan(&identifier); err != nil {
		t.Fatal(err)
	}
	lab.record.check("restored into a new cluster", "system identifier differs from destroyed cluster", identifier,
		identifier != lab.config.SystemIdentifier)
	lab.config.SystemIdentifier = identifier
	experimentShared.SystemIdentifier = identifier
	lab.record.observe("restore_output", lab.tool(t, "postgres-backup-local", "restore", backupPath))
}

func outboxPublished(t *testing.T, lab *experimentLab, job int64) bool {
	t.Helper()
	return readOutbox(t, lab, job).Published
}

func TestIssue118E12SnapshotRecovery(t *testing.T) {
	config := requireExperimentConfig(t)
	if os.Getenv("ISSUE118_ALLOW_DESTROY") != config.PostgresProject {
		t.Skipf("destructive snapshot experiment requires ISSUE118_ALLOW_DESTROY=%s", config.PostgresProject)
	}
	lab := openExperimentLab(t, "e12-snapshot-recovery",
		"EXTERNAL destructive loss of the isolated PostgreSQL volume, migration-first logical restore; ACTUAL API and worker processes",
		"Which accepted, completed, and post-backup effects survive snapshot recovery, and can stale broker messages alias restored identities?")
	api, baseURL := startAPI(t, lab.config, lab.record, "e12-api-a")
	create := func(title string) int64 {
		status, body := httpJSON(t, http.MethodPost, baseURL+"/items", map[string]string{"title": title})
		if status != http.StatusCreated {
			t.Fatalf("create %q: %d %v", title, status, body)
		}
		return jsonID(t, body, "id")
	}
	accept := func(item int64) int64 {
		status, body := httpJSON(t, http.MethodPost, fmt.Sprintf("%s/items/%d/process", baseURL, item), nil)
		if status != http.StatusAccepted {
			t.Fatalf("accept %d: %d %v", item, status, body)
		}
		return jsonID(t, body, "id")
	}
	waitPublished := func(step string, job int64) {
		waitUntil(t, lab.record, step, fmt.Sprintf("outbox for job %d published", job), func() (bool, any, error) {
			return outboxPublished(t, lab, job), nil, nil
		})
	}

	// Phase 1: build the recovery point.
	worker := startWorker(t, lab.config, lab.record, "e12-w1", nil)
	itemB := create("issue118 E12 B done before backup")
	jobB := accept(itemB)
	waitForState(t, lab.record, lab.admin, itemB, "B completes", "done once", func(s businessState) bool { return isCompletedOnce(s, jobB) })
	waitForQueue(t, lab.config, lab.record, "B acknowledged", queueCounts{Consumers: 1})
	stopGracefully(t, lab.record, worker)
	itemC := create("issue118 E12 C published before backup")
	jobC := accept(itemC)
	waitPublished("C published", jobC)
	waitForQueue(t, lab.config, lab.record, "C message waiting", queueCounts{Ready: 1})
	lab.rabbitAction(t, "stop")
	itemA := create("issue118 E12 A unpublished at backup")
	jobA := accept(itemA)

	backupPath := filepath.Join(t.TempDir(), "issue118-e12.dump")
	lab.tool(t, "postgres-backup-local", "create", backupPath)
	lab.tool(t, "postgres-backup-local", "validate", backupPath)
	backupB := checkState(t, lab.record, lab.admin, itemB, "backup point: B", "done once", func(s businessState) bool { return isCompletedOnce(s, jobB) })
	checkState(t, lab.record, lab.admin, itemC, "backup point: C", "accepted", func(s businessState) bool { return isUntouched(s, jobC) })
	checkState(t, lab.record, lab.admin, itemA, "backup point: A", "accepted", func(s businessState) bool { return isUntouched(s, jobA) })
	lab.record.check("backup point: A outbox unpublished, C published", "A=false C=true",
		fmt.Sprintf("A=%t C=%t", outboxPublished(t, lab, jobA), outboxPublished(t, lab, jobC)),
		!outboxPublished(t, lab, jobA) && outboxPublished(t, lab, jobC))
	backupSequences := readSequences(t, lab)
	lab.record.observe("backup_sequences", backupSequences)

	// Phase 2: work after the recovery point, which restoration will lose.
	lab.rabbitAction(t, "start")
	waitPublished("A published after backup", jobA)
	worker = startWorker(t, lab.config, lab.record, "e12-w2", nil)
	waitForState(t, lab.record, lab.admin, itemA, "A completes after backup", "done", func(s businessState) bool { return isCompletedOnce(s, jobA) })
	waitForState(t, lab.record, lab.admin, itemC, "C completes after backup", "done", func(s businessState) bool { return isCompletedOnce(s, jobC) })
	waitForQueue(t, lab.config, lab.record, "A and C acknowledged", queueCounts{Consumers: 1})
	stopGracefully(t, lab.record, worker)
	waitForQueue(t, lab.config, lab.record, "no consumer", queueCounts{})
	itemD := create("issue118 E12 D created after backup")
	jobD := accept(itemD)
	waitPublished("D published", jobD)
	waitForQueue(t, lab.config, lab.record, "D message left in broker", queueCounts{Ready: 1})
	stopGracefully(t, lab.record, api)
	lab.record.observe("ids", map[string]int64{"item_a": itemA, "job_a": jobA, "item_b": itemB, "job_b": jobB,
		"item_c": itemC, "job_c": jobC, "item_d": itemD, "job_d": jobD})

	// Phase 3: destructive loss and migration-first restore.
	lab.destroyAndRestore(t, backupPath)
	restoredB := checkState(t, lab.record, lab.admin, itemB, "restored B", "identical to backup",
		func(s businessState) bool { return s.String() == backupB.String() })
	checkState(t, lab.record, lab.admin, itemC, "restored C (completion after backup lost)", "pending; accepted; no result",
		func(s businessState) bool { return isUntouched(s, jobC) })
	checkState(t, lab.record, lab.admin, itemA, "restored A", "pending; accepted; no result",
		func(s businessState) bool { return isUntouched(s, jobA) })
	lab.record.check("restored outbox: A unpublished, C published", "A=false C=true",
		fmt.Sprintf("A=%t C=%t", outboxPublished(t, lab, jobA), outboxPublished(t, lab, jobC)),
		!outboxPublished(t, lab, jobA) && outboxPublished(t, lab, jobC))
	var dRows int
	if err := lab.admin.QueryRow(context.Background(), "SELECT count(*) FROM public.work_items WHERE id=$1", itemD).Scan(&dRows); err != nil {
		t.Fatal(err)
	}
	lab.record.check("post-backup item D absent", 0, dRows, dRows == 0)
	restoredSequences := readSequences(t, lab)
	lab.record.check("sequences rewound to the recovery point", backupSequences, restoredSequences,
		fmt.Sprint(backupSequences) == fmt.Sprint(restoredSequences))
	waitForQueue(t, lab.config, lab.record, "stale D message still in broker", queueCounts{Ready: 1})

	// Phase 4: create new work while the stale message waits, with publication
	// disabled so only the stale message can reach the worker.
	lab.rabbitAction(t, "stop")
	api, baseURL = startAPI(t, lab.config, lab.record, "e12-api-b")
	itemE := create("issue118 E12 E created after restore")
	jobE := accept(itemE)
	stopGracefully(t, lab.record, api)
	collision := itemE == itemD && jobE == jobD
	lab.record.observe("identity_alias", map[string]any{"stale_message": map[string]int64{"work_item_id": itemD, "job_id": jobD},
		"new_work": map[string]int64{"work_item_id": itemE, "job_id": jobE}, "collision": collision})
	lab.record.check("rewound sequences reissue the stale message's identities to new work",
		fmt.Sprintf("item=%d job=%d", itemD, jobD), fmt.Sprintf("item=%d job=%d", itemE, jobE), collision)
	lab.rabbitAction(t, "start")
	waitForQueue(t, lab.config, lab.record, "only the stale message is queued", queueCounts{Ready: 1})
	lab.record.check("E and A outbox rows unpublished", "false/false",
		fmt.Sprintf("%t/%t", outboxPublished(t, lab, jobE), outboxPublished(t, lab, jobA)),
		!outboxPublished(t, lab, jobE) && !outboxPublished(t, lab, jobA))
	worker = startWorker(t, lab.config, lab.record, "e12-w3", nil)
	waitForQueue(t, lab.config, lab.record, "stale message settled", queueCounts{Consumers: 1})
	stale := checkState(t, lab.record, lab.admin, itemE, "E after the stale message", "invariant holds", func(businessState) bool { return true })
	staleCompleted := isCompletedOnce(stale, jobE)
	lab.record.observe("stale_message_outcome", map[string]any{"completed_new_job": staleCompleted,
		"new_job_outbox_published": outboxPublished(t, lab, jobE), "result_title": stale.Results})
	lab.record.check("stale message for lost job D processed new job E", "E done while its own outbox row is unpublished",
		stale, staleCompleted && !outboxPublished(t, lab, jobE) && stale.Results[0].InputTitle == stale.Title)

	// Resume publication: A recovers from its restored outbox row; E's own
	// message is redundant.
	api, baseURL = startAPI(t, lab.config, lab.record, "e12-api-c")
	waitForState(t, lab.record, lab.admin, itemA, "A recovers from restored outbox", "done once",
		func(s businessState) bool { return isCompletedOnce(s, jobA) })
	waitPublished("A outbox republished", jobA)
	waitPublished("E outbox published", jobE)
	waitForState(t, lab.record, lab.admin, itemE, "E done", "done once", func(s businessState) bool { return isCompletedOnce(s, jobE) })
	waitForQueue(t, lab.config, lab.record, "all messages acknowledged", queueCounts{Consumers: 1})
	stopGracefully(t, lab.record, worker)
	waitForQueue(t, lab.config, lab.record, "quiet queue", queueCounts{})

	checkState(t, lab.record, lab.admin, itemC, "C blocked after restore", "accepted job with published outbox and no message",
		func(s businessState) bool { return isUntouched(s, jobC) && outboxPublished(t, lab, jobC) })
	status, body := httpJSON(t, http.MethodPost, fmt.Sprintf("%s/items/%d/process", baseURL, itemC), nil)
	lab.record.observe("c_new_request", map[string]any{"status": status, "body": body})
	lab.record.check("new request for C is refused while blocked", "409 processing_already_active job "+fmt.Sprint(jobC),
		fmt.Sprint(status, body), status == http.StatusConflict && body["error"] == "processing_already_active" && jsonID(t, body, "processing_job_id") == jobC)
	checkState(t, lab.record, lab.admin, itemB, "B after recovery", "unchanged", func(s businessState) bool { return s.String() == restoredB.String() })
	stopGracefully(t, lab.record, api)
	present, err := presentProtectedVolumes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lab.record.check("protected lab volumes still present", lab.config.PresentProtected, present, slices.Equal(present, lab.config.PresentProtected))
}
