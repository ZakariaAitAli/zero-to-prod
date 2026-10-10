//go:build crashexperiment

// Issue #118 crash-consistency harness. These experiments run the real worker
// and API binaries against an isolated PostgreSQL/RabbitMQ lab, signal those
// processes at observed blocking points, and record sanitized JSON evidence.
// They never run in the default test build.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	experimentProjectPrefix = "zero-to-prod-118"
	commitGateLockKey       = 118118
	outboxGateLockKey       = 118119
	itemLockKey             = 1464423501
	observationPoll         = 10 * time.Millisecond
	observationTimeout      = 20 * time.Second
)

// Lab resources that destructive experiments must never touch.
var protectedPostgresPorts = []string{"55432", "55433"}
var protectedAMQPPorts = []string{"5672"}
var protectedVolumes = []string{
	"zero-to-prod-local_postgres_data",
	"zero-to-prod-117_postgres_data",
	"zero-to-prod-rabbitmq_rabbitmq_data",
}

type experimentConfig struct {
	RepoRoot           string
	EvidenceDir        string
	PostgresProject    string
	RabbitMQProject    string
	PostgresPort       string
	AMQPPort           string
	AdminURL           string
	FixtureURL         string
	WorkerURL          string
	AppURL             string
	RabbitWorkerURL    string
	RabbitFixtureURL   string
	RabbitPublisherURL string
	Queue              string
	PostgresContainer  string
	RabbitMQContainer  string
	SystemIdentifier   string
	WorkerBinary       string
	APIBinary          string
	PresentProtected   []string
}

var (
	experimentOnce   sync.Once
	experimentShared experimentConfig
	experimentErr    error
	binaryDir        string
)

func TestMain(m *testing.M) {
	code := m.Run()
	if binaryDir != "" {
		_ = os.RemoveAll(binaryDir)
	}
	os.Exit(code)
}

func requiredEnv(names ...string) (map[string]string, error) {
	values := map[string]string{}
	var missing []string
	for _, name := range names {
		values[name] = os.Getenv(name)
		if values[name] == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing environment: %s", strings.Join(missing, ", "))
	}
	return values, nil
}

func endpoint(raw string) (host, port, database string, err error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", "", err
	}
	return parsed.Hostname(), parsed.Port(), strings.TrimPrefix(parsed.Path, "/"), nil
}

func command(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, sanitize(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

// composeContainer resolves exactly one container by Compose project and
// service labels and verifies its host port binding.
func composeContainer(ctx context.Context, project, service, containerPort, hostPort string) (string, error) {
	ids, err := command(ctx, "", nil, "docker", "ps", "-a", "--no-trunc", "-q",
		"--filter", "label=com.docker.compose.project="+project,
		"--filter", "label=com.docker.compose.service="+service)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(ids)
	if len(fields) != 1 {
		return "", fmt.Errorf("expected one %s container in project %s, found %d", service, project, len(fields))
	}
	binding, err := command(ctx, "", nil, "docker", "port", fields[0], containerPort)
	if err != nil {
		return "", err
	}
	if binding != "127.0.0.1:"+hostPort {
		return "", fmt.Errorf("%s container %s binds %q, expected 127.0.0.1:%s", service, project, binding, hostPort)
	}
	return fields[0], nil
}

func loadExperimentConfig() (experimentConfig, error) {
	env, err := requiredEnv("ZTP_COMPOSE_PROJECT_NAME", "ZTP_RABBITMQ_COMPOSE_PROJECT_NAME",
		"ISSUE118_ADMIN_DATABASE_URL", "ISSUE118_EVIDENCE_DIR", "WORKER_FIXTURE_DATABASE_URL",
		"WORKER_DATABASE_URL", "OUTBOX_INTEGRATION_DATABASE_URL", "RABBITMQ_WORKER_URL",
		"RABBITMQ_FIXTURE_URL", "RABBITMQ_PUBLISHER_URL", "RABBITMQ_QUEUE")
	if err != nil {
		return experimentConfig{}, err
	}
	config := experimentConfig{
		PostgresProject: env["ZTP_COMPOSE_PROJECT_NAME"], RabbitMQProject: env["ZTP_RABBITMQ_COMPOSE_PROJECT_NAME"],
		AdminURL: env["ISSUE118_ADMIN_DATABASE_URL"], FixtureURL: env["WORKER_FIXTURE_DATABASE_URL"],
		WorkerURL: env["WORKER_DATABASE_URL"], AppURL: env["OUTBOX_INTEGRATION_DATABASE_URL"],
		RabbitWorkerURL: env["RABBITMQ_WORKER_URL"], RabbitFixtureURL: env["RABBITMQ_FIXTURE_URL"],
		RabbitPublisherURL: env["RABBITMQ_PUBLISHER_URL"], Queue: env["RABBITMQ_QUEUE"],
	}
	for _, project := range []string{config.PostgresProject, config.RabbitMQProject} {
		if !strings.HasPrefix(project, experimentProjectPrefix) {
			return experimentConfig{}, fmt.Errorf("refusing non-isolated Compose project %q", project)
		}
	}
	if config.PostgresProject == config.RabbitMQProject {
		return experimentConfig{}, errors.New("PostgreSQL and RabbitMQ projects must differ")
	}
	for _, raw := range []string{config.AdminURL, config.FixtureURL, config.WorkerURL, config.AppURL} {
		host, port, database, err := endpoint(raw)
		if err != nil {
			return experimentConfig{}, err
		}
		if host != "127.0.0.1" || database != "zero_to_prod" || slices.Contains(protectedPostgresPorts, port) {
			return experimentConfig{}, fmt.Errorf("refusing PostgreSQL endpoint %s:%s/%s", host, port, database)
		}
		if config.PostgresPort == "" {
			config.PostgresPort = port
		} else if port != config.PostgresPort {
			return experimentConfig{}, errors.New("PostgreSQL URLs target different ports")
		}
	}
	for _, raw := range []string{config.RabbitWorkerURL, config.RabbitFixtureURL, config.RabbitPublisherURL} {
		host, port, _, err := endpoint(raw)
		if err != nil {
			return experimentConfig{}, err
		}
		if host != "127.0.0.1" || slices.Contains(protectedAMQPPorts, port) {
			return experimentConfig{}, fmt.Errorf("refusing RabbitMQ endpoint %s:%s", host, port)
		}
		if config.AMQPPort == "" {
			config.AMQPPort = port
		} else if port != config.AMQPPort {
			return experimentConfig{}, errors.New("RabbitMQ URLs target different ports")
		}
	}
	if os.Getenv("ZTP_POSTGRES_PORT") != config.PostgresPort {
		return experimentConfig{}, errors.New("ZTP_POSTGRES_PORT does not match the experiment database URLs")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if config.PostgresContainer, err = composeContainer(ctx, config.PostgresProject, "postgres", "5432/tcp", config.PostgresPort); err != nil {
		return experimentConfig{}, err
	}
	if config.RabbitMQContainer, err = composeContainer(ctx, config.RabbitMQProject, "rabbitmq", "5672/tcp", config.AMQPPort); err != nil {
		return experimentConfig{}, err
	}
	// Prove the URL endpoint is the validated container, not merely a port.
	connection, err := pgx.Connect(ctx, config.AdminURL)
	if err != nil {
		return experimentConfig{}, err
	}
	defer connection.Close(ctx)
	if err := connection.QueryRow(ctx, "SELECT system_identifier::text FROM pg_control_system()").Scan(&config.SystemIdentifier); err != nil {
		return experimentConfig{}, err
	}
	inside, err := command(ctx, "", nil, "docker", "exec", config.PostgresContainer, "psql", "-U", "zero_to_prod_admin",
		"-d", "zero_to_prod", "-Atc", "SELECT system_identifier FROM pg_control_system()")
	if err != nil {
		return experimentConfig{}, err
	}
	if inside != config.SystemIdentifier {
		return experimentConfig{}, fmt.Errorf("database URL system identifier %s differs from container %s", config.SystemIdentifier, inside)
	}

	if config.RepoRoot, err = filepath.Abs("../../.."); err != nil {
		return experimentConfig{}, err
	}
	if _, err := os.Stat(filepath.Join(config.RepoRoot, "tools", "postgres-local")); err != nil {
		return experimentConfig{}, fmt.Errorf("repository root not found: %w", err)
	}
	if config.EvidenceDir, err = filepath.Abs(env["ISSUE118_EVIDENCE_DIR"]); err != nil {
		return experimentConfig{}, err
	}
	if err := os.MkdirAll(config.EvidenceDir, 0o755); err != nil {
		return experimentConfig{}, err
	}
	if config.PresentProtected, err = presentProtectedVolumes(ctx); err != nil {
		return experimentConfig{}, err
	}

	if binaryDir, err = os.MkdirTemp("", "issue118-binaries-"); err != nil {
		return experimentConfig{}, err
	}
	config.WorkerBinary = filepath.Join(binaryDir, "worker")
	config.APIBinary = filepath.Join(binaryDir, "api")
	if _, err := command(ctx, ".", nil, "go", "build", "-trimpath", "-o", config.WorkerBinary, "."); err != nil {
		return experimentConfig{}, err
	}
	if _, err := command(ctx, ".", nil, "go", "build", "-trimpath", "-o", config.APIBinary, "../api"); err != nil {
		return experimentConfig{}, err
	}
	return config, nil
}

func presentProtectedVolumes(ctx context.Context) ([]string, error) {
	output, err := command(ctx, "", nil, "docker", "volume", "ls", "--format", "{{.Name}}")
	if err != nil {
		return nil, err
	}
	var present []string
	for _, name := range strings.Fields(output) {
		if slices.Contains(protectedVolumes, name) {
			present = append(present, name)
		}
	}
	return present, nil
}

// requireExperimentConfig validates isolation once per test binary.
func requireExperimentConfig(t *testing.T) experimentConfig {
	t.Helper()
	experimentOnce.Do(func() { experimentShared, experimentErr = loadExperimentConfig() })
	if experimentErr != nil {
		t.Fatalf("experiment isolation validation failed: %v", experimentErr)
	}
	return experimentShared
}

// --- evidence -------------------------------------------------------------

var credentialPattern = regexp.MustCompile(`([a-z]+://)[^@\s/]+@`)

func sanitize(text string) string {
	return credentialPattern.ReplaceAllString(text, "${1}<redacted>@")
}

type syncEvidence struct {
	Step        string `json:"step"`
	Condition   string `json:"condition"`
	ElapsedMS   int64  `json:"elapsed_ms"`
	Observation any    `json:"observation,omitempty"`
}

type processEvidence struct {
	Name       string   `json:"name"`
	Signals    []string `json:"signals"`
	ExitStatus string   `json:"exit_status"`
	LogLines   int      `json:"log_lines"`
	LogExcerpt []string `json:"log_excerpt"`
}

type checkEvidence struct {
	Name     string `json:"name"`
	Expected any    `json:"expected"`
	Observed any    `json:"observed"`
	Passed   bool   `json:"passed"`
}

type experimentRecord struct {
	t               *testing.T
	start           time.Time
	mu              sync.Mutex
	ID              string            `json:"id"`
	Classification  string            `json:"classification"`
	Question        string            `json:"question"`
	Environment     map[string]string `json:"environment"`
	Synchronization []syncEvidence    `json:"synchronization"`
	Checks          []checkEvidence   `json:"checks"`
	Observations    map[string]any    `json:"observations"`
	Processes       []processEvidence `json:"processes"`
	Passed          bool              `json:"passed"`
	DurationMS      int64             `json:"duration_ms"`
	RecordedAt      string            `json:"recorded_at"`
}

func newExperimentRecord(t *testing.T, config experimentConfig, id, classification, question string) *experimentRecord {
	record := &experimentRecord{
		t: t, start: time.Now(), ID: id, Classification: classification, Question: question,
		Environment: map[string]string{
			"classification":           "LOCAL-FIRST",
			"postgres_compose_project": config.PostgresProject,
			"rabbitmq_compose_project": config.RabbitMQProject,
			"postgres_port":            config.PostgresPort,
			"amqp_port":                config.AMQPPort,
		},
		Observations: map[string]any{},
	}
	t.Cleanup(func() {
		record.mu.Lock()
		defer record.mu.Unlock()
		record.Passed = !t.Failed()
		record.DurationMS = time.Since(record.start).Milliseconds()
		record.RecordedAt = time.Now().UTC().Format(time.RFC3339)
		encoded, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			t.Errorf("encode evidence: %v", err)
			return
		}
		path := filepath.Join(config.EvidenceDir, id+".json")
		if err := os.WriteFile(path, append([]byte(sanitize(string(encoded))), '\n'), 0o644); err != nil {
			t.Errorf("write evidence: %v", err)
		}
	})
	return record
}

func (record *experimentRecord) sync(step, condition string, observation any) {
	record.mu.Lock()
	defer record.mu.Unlock()
	record.Synchronization = append(record.Synchronization, syncEvidence{
		Step: step, Condition: condition, ElapsedMS: time.Since(record.start).Milliseconds(), Observation: observation})
}

func (record *experimentRecord) observe(key string, value any) {
	record.mu.Lock()
	defer record.mu.Unlock()
	record.Observations[key] = value
}

// check records expected versus observed state and fails the experiment
// immediately when an invariant does not hold.
func (record *experimentRecord) check(name string, expected, observed any, passed bool) {
	record.t.Helper()
	record.mu.Lock()
	record.Checks = append(record.Checks, checkEvidence{Name: name, Expected: expected, Observed: observed, Passed: passed})
	record.mu.Unlock()
	if !passed {
		record.t.Fatalf("%s: expected %v, observed %v", name, expected, observed)
	}
}

func (record *experimentRecord) process(process *managedProcess) {
	record.mu.Lock()
	defer record.mu.Unlock()
	record.Processes = append(record.Processes, process.evidence())
}

// --- managed processes ----------------------------------------------------

type logLine struct {
	At   time.Duration
	Text string
}

type managedProcess struct {
	Name    string
	cmd     *exec.Cmd
	started time.Time
	done    chan struct{}
	mu      sync.Mutex
	lines   []logLine
	signals []string
	status  string
}

func startManagedProcess(t *testing.T, name, binary string, env []string) *managedProcess {
	t.Helper()
	process := &managedProcess{Name: name, done: make(chan struct{})}
	process.cmd = exec.Command(binary)
	process.cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}, env...)
	reader, writer := io.Pipe()
	process.cmd.Stdout = writer
	process.cmd.Stderr = writer
	if err := process.cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", name, err)
	}
	process.started = time.Now()
	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			process.mu.Lock()
			process.lines = append(process.lines, logLine{At: time.Since(process.started), Text: sanitize(scanner.Text())})
			process.mu.Unlock()
		}
	}()
	go func() {
		err := process.cmd.Wait()
		_ = writer.Close()
		process.mu.Lock()
		process.status = describeExit(process.cmd.ProcessState, err)
		process.mu.Unlock()
		close(process.done)
	}()
	t.Cleanup(func() {
		select {
		case <-process.done:
		default:
			_ = process.cmd.Process.Signal(syscall.SIGKILL)
			<-process.done
		}
	})
	return process
}

func describeExit(state *os.ProcessState, err error) string {
	if state == nil {
		return fmt.Sprintf("wait error: %v", err)
	}
	if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return "signal=" + status.Signal().String()
	}
	return "exit_code=" + strconv.Itoa(state.ExitCode())
}

func (process *managedProcess) signal(t *testing.T, signal syscall.Signal) {
	t.Helper()
	process.mu.Lock()
	process.signals = append(process.signals, signal.String())
	process.mu.Unlock()
	if err := process.cmd.Process.Signal(signal); err != nil {
		t.Fatalf("signal %s to %s: %v", signal, process.Name, err)
	}
}

// waitExit returns the exit status and how long the process took to exit.
func (process *managedProcess) waitExit(t *testing.T, timeout time.Duration) (string, time.Duration) {
	t.Helper()
	start := time.Now()
	select {
	case <-process.done:
	case <-time.After(timeout):
		t.Fatalf("%s did not exit within %s", process.Name, timeout)
	}
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.status, time.Since(start)
}

func (process *managedProcess) running() bool {
	select {
	case <-process.done:
		return false
	default:
		return true
	}
}

// kernelState reads the scheduler state from /proc ("T" means stopped).
func (process *managedProcess) kernelState() string {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", process.cmd.Process.Pid))
	if err != nil {
		return "gone"
	}
	fields := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
	if len(fields) == 0 {
		return "unknown"
	}
	return fields[0]
}

func (process *managedProcess) linesContaining(substring string) []logLine {
	process.mu.Lock()
	defer process.mu.Unlock()
	var matches []logLine
	for _, line := range process.lines {
		if strings.Contains(line.Text, substring) {
			matches = append(matches, line)
		}
	}
	return matches
}

func (process *managedProcess) evidence() processEvidence {
	process.mu.Lock()
	defer process.mu.Unlock()
	excerpt := make([]string, 0, 30)
	for index, line := range process.lines {
		if index >= 30 {
			excerpt = append(excerpt, fmt.Sprintf("... %d more lines", len(process.lines)-index))
			break
		}
		excerpt = append(excerpt, fmt.Sprintf("+%dms %s", line.At.Milliseconds(), line.Text))
	}
	status := process.status
	if status == "" {
		status = "running"
	}
	return processEvidence{Name: process.Name, Signals: slices.Clone(process.signals), ExitStatus: status,
		LogLines: len(process.lines), LogExcerpt: excerpt}
}

func withParams(rawURL string, params map[string]string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	query := parsed.Query()
	for key, value := range params {
		query.Set(key, value)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func appName(name string) string { return "issue118-" + name }

func startWorker(t *testing.T, config experimentConfig, record *experimentRecord, name string, extra map[string]string) *managedProcess {
	t.Helper()
	params := map[string]string{"application_name": appName(name)}
	for key, value := range extra {
		params[key] = value
	}
	before, err := rabbitQueueCounts(config)
	if err != nil {
		t.Fatal(err)
	}
	process := startManagedProcess(t, name, config.WorkerBinary, []string{
		"DATABASE_URL=" + withParams(config.WorkerURL, params),
		"RABBITMQ_WORKER_URL=" + config.RabbitWorkerURL,
		"RABBITMQ_QUEUE=" + config.Queue,
	})
	t.Cleanup(func() { record.process(process) })
	waitUntil(t, record, "start "+name, fmt.Sprintf("queue consumers > %d", before.Consumers), func() (bool, any, error) {
		counts, err := rabbitQueueCounts(config)
		return err == nil && counts.Consumers > before.Consumers, counts, err
	})
	return process
}

func startAPI(t *testing.T, config experimentConfig, record *experimentRecord, name string) (*managedProcess, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	_ = listener.Close()
	process := startManagedProcess(t, name, config.APIBinary, []string{
		"PORT=" + port,
		"DATABASE_URL=" + withParams(config.AppURL, map[string]string{"application_name": appName(name)}),
		"RABBITMQ_PUBLISHER_URL=" + config.RabbitPublisherURL,
		"RABBITMQ_QUEUE=" + config.Queue,
	})
	t.Cleanup(func() { record.process(process) })
	baseURL := "http://127.0.0.1:" + port
	waitUntil(t, record, "start "+name, "GET /ready returns 200", func() (bool, any, error) {
		response, err := http.Get(baseURL + "/ready")
		if err != nil {
			return false, nil, nil
		}
		response.Body.Close()
		return response.StatusCode == http.StatusOK, response.StatusCode, nil
	})
	return process, baseURL
}

func stopGracefully(t *testing.T, record *experimentRecord, process *managedProcess) {
	t.Helper()
	process.signal(t, syscall.SIGTERM)
	status, elapsed := process.waitExit(t, 15*time.Second)
	record.observe(process.Name+"_graceful_exit", map[string]any{"status": status, "elapsed_ms": elapsed.Milliseconds()})
	record.check(process.Name+" graceful exit status", "exit_code=0", status, status == "exit_code=0")
}

func killProcess(t *testing.T, record *experimentRecord, process *managedProcess) {
	t.Helper()
	process.signal(t, syscall.SIGKILL)
	status, _ := process.waitExit(t, 10*time.Second)
	record.check(process.Name+" exit status after SIGKILL", "signal=killed", status, status == "signal=killed")
}

// --- observation ------------------------------------------------------------

// waitUntil polls an observable condition. It is the only synchronization
// primitive used before signalling processes; it never relies on elapsed time.
func waitUntil(t *testing.T, record *experimentRecord, step, condition string, probe func() (bool, any, error)) any {
	t.Helper()
	deadline := time.Now().Add(observationTimeout)
	var lastObservation any
	var lastErr error
	for time.Now().Before(deadline) {
		ok, observation, err := probe()
		lastObservation, lastErr = observation, err
		if err == nil && ok {
			record.sync(step, condition, observation)
			return observation
		}
		time.Sleep(observationPoll)
	}
	record.sync(step, "TIMEOUT: "+condition, map[string]any{"last": lastObservation, "error": fmt.Sprint(lastErr)})
	t.Fatalf("%s: condition not observed: %s (last=%v err=%v)", step, condition, lastObservation, lastErr)
	return nil
}

// observeWithin is a measurement, not synchronization: it reports whether a
// condition appeared within a bounded window and does not fail on timeout.
func observeWithin(window time.Duration, probe func() (bool, any, error)) (bool, any, time.Duration) {
	start := time.Now()
	var last any
	for time.Since(start) < window {
		ok, observation, err := probe()
		last = observation
		if err == nil && ok {
			return true, observation, time.Since(start)
		}
		time.Sleep(observationPoll)
	}
	return false, last, time.Since(start)
}

type backendObservation struct {
	PID           int32   `json:"pid"`
	State         string  `json:"state"`
	WaitEventType string  `json:"wait_event_type"`
	WaitEvent     string  `json:"wait_event"`
	Query         string  `json:"query"`
	XID           string  `json:"backend_xid"`
	BlockedBy     []int32 `json:"blocked_by"`
}

func backends(ctx context.Context, admin *pgxpool.Pool, application string) ([]backendObservation, error) {
	rows, err := admin.Query(ctx, `SELECT pid, coalesce(state,''), coalesce(wait_event_type,''), coalesce(wait_event,''),
 left(regexp_replace(query, '\s+', ' ', 'g'), 160), coalesce(backend_xid::text,''), pg_blocking_pids(pid)
 FROM pg_stat_activity WHERE application_name=$1 ORDER BY pid`, application)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (backendObservation, error) {
		var observation backendObservation
		err := row.Scan(&observation.PID, &observation.State, &observation.WaitEventType, &observation.WaitEvent,
			&observation.Query, &observation.XID, &observation.BlockedBy)
		return observation, err
	})
}

// waitForBackend waits until one backend of the application matches.
func waitForBackend(t *testing.T, record *experimentRecord, admin *pgxpool.Pool, application, step, condition string,
	match func(backendObservation) bool) backendObservation {
	t.Helper()
	var found backendObservation
	waitUntil(t, record, step, condition, func() (bool, any, error) {
		observed, err := backends(context.Background(), admin, application)
		for _, backend := range observed {
			if match(backend) {
				found = backend
				return true, backend, err
			}
		}
		return false, observed, err
	})
	return found
}

func waitForNoBackend(t *testing.T, record *experimentRecord, admin *pgxpool.Pool, application, step string) {
	t.Helper()
	waitUntil(t, record, step, "no PostgreSQL backend for "+application, func() (bool, any, error) {
		observed, err := backends(context.Background(), admin, application)
		return err == nil && len(observed) == 0, observed, err
	})
}

func blockedBy(backend backendObservation, pid int32) bool {
	return slices.Contains(backend.BlockedBy, pid)
}

type queueCounts struct {
	Ready     int `json:"ready"`
	Unacked   int `json:"unacknowledged"`
	Consumers int `json:"consumers"`
}

// rabbitQueueCounts asks the queue process itself (not the lagging management
// statistics) for ready, unacknowledged, and consumer counts.
func rabbitQueueCounts(config experimentConfig) (queueCounts, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	output, err := command(ctx, "", nil, "docker", "exec", config.RabbitMQContainer, "rabbitmqctl", "-q", "-p", "zero_to_prod",
		"list_queues", "--no-table-headers", "name", "messages_ready", "messages_unacknowledged", "consumers")
	if err != nil {
		return queueCounts{}, err
	}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 4 && fields[0] == config.Queue {
			var counts queueCounts
			_, err := fmt.Sscan(strings.Join(fields[1:], " "), &counts.Ready, &counts.Unacked, &counts.Consumers)
			return counts, err
		}
	}
	return queueCounts{}, fmt.Errorf("queue %s not listed", config.Queue)
}

func waitForQueue(t *testing.T, config experimentConfig, record *experimentRecord, step string, want queueCounts) {
	t.Helper()
	waitUntil(t, record, step, fmt.Sprintf("queue ready=%d unacknowledged=%d consumers=%d", want.Ready, want.Unacked, want.Consumers),
		func() (bool, any, error) {
			counts, err := rabbitQueueCounts(config)
			return err == nil && counts == want, counts, err
		})
}

func waitForState(t *testing.T, record *experimentRecord, admin *pgxpool.Pool, workItemID int64, step, condition string,
	match func(businessState) bool) businessState {
	t.Helper()
	var state businessState
	waitUntil(t, record, step, condition, func() (bool, any, error) {
		observed, err := readBusinessState(context.Background(), admin, workItemID)
		if err != nil {
			return false, nil, err
		}
		state = observed
		return match(observed), observed, nil
	})
	return state
}

func isUntouched(state businessState, jobID int64) bool {
	job, ok := state.job(jobID)
	return state.Status == "pending" && ok && job.State == "accepted" && job.AttemptCount == 0 && len(state.Results) == 0
}

func isCompletedOnce(state businessState, jobID int64) bool {
	job, ok := state.job(jobID)
	return state.Status == "done" && ok && job.State == "succeeded" && job.AttemptCount == 1 &&
		len(state.Results) == 1 && state.Results[0].ProcessingJobID == jobID
}

// checkState records the full state, the ADR 0003 invariant, and the
// experiment-specific expectation.
func checkState(t *testing.T, record *experimentRecord, admin *pgxpool.Pool, workItemID int64, name, expected string,
	match func(businessState) bool) businessState {
	t.Helper()
	state, err := readBusinessState(context.Background(), admin, workItemID)
	if err != nil {
		t.Fatal(err)
	}
	invariant := successInvariantViolation(state)
	record.check(name+": ADR 0003 success invariant", "holds", fmt.Sprint(invariant), invariant == nil)
	record.check(name, expected, state, match(state))
	return state
}

// tuplesWrittenBy counts heap tuples (including uncommitted versions) created
// by a transaction, proving which writes a blocked transaction has executed.
func tuplesWrittenBy(ctx context.Context, admin *pgxpool.Pool, relation, xid string) (int, error) {
	var count int
	err := admin.QueryRow(ctx, `SELECT count(*)
 FROM generate_series(0, (pg_relation_size($1::text::regclass) / current_setting('block_size')::int) - 1) AS block,
 LATERAL issue118.heap_page_items(issue118.get_raw_page($1::text, block::int)) AS tuple
 WHERE tuple.t_xmin::text = $2`, relation, xid).Scan(&count)
	return count, err
}

func writesBy(ctx context.Context, admin *pgxpool.Pool, xid string) (map[string]int, error) {
	writes := map[string]int{}
	for _, relation := range []string{"public.work_item_results", "public.work_items", "public.processing_jobs"} {
		count, err := tuplesWrittenBy(ctx, admin, relation, xid)
		if err != nil {
			return nil, err
		}
		writes[relation] = count
	}
	return writes, nil
}

// --- fixtures and instrumentation --------------------------------------------

type experimentLab struct {
	config  experimentConfig
	record  *experimentRecord
	admin   *pgxpool.Pool
	fixture *pgxpool.Pool
}

func openExperimentLab(t *testing.T, id, classification, question string) *experimentLab {
	t.Helper()
	config := requireExperimentConfig(t)
	record := newExperimentRecord(t, config, id, classification, question)
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, config.AdminURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	fixture, err := pgxpool.New(ctx, config.FixtureURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fixture.Close)
	// Inspection-only helpers live in a separate schema of the isolated database.
	if _, err := admin.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS issue118;
 CREATE EXTENSION IF NOT EXISTS pageinspect SCHEMA issue118;
 GRANT USAGE ON SCHEMA issue118 TO PUBLIC;`); err != nil {
		t.Fatal(err)
	}
	var checkInterval string
	if err := admin.QueryRow(ctx, "SELECT current_setting('client_connection_check_interval')").Scan(&checkInterval); err != nil {
		t.Fatal(err)
	}
	record.Environment["server_client_connection_check_interval"] = checkInterval
	queue, err := rabbitQueueCounts(config)
	if err != nil {
		t.Fatal(err)
	}
	record.check("precondition: isolated queue is quiet", queueCounts{}, queue, queue == queueCounts{})
	t.Cleanup(func() {
		if t.Failed() {
			// Leave the isolated queue quiet for later experiments.
			_, _ = command(context.Background(), "", nil, "docker", "exec", config.RabbitMQContainer,
				"rabbitmqctl", "-p", "zero_to_prod", "purge_queue", config.Queue)
		}
	})
	return &experimentLab{config: config, record: record, admin: admin, fixture: fixture}
}

// createAcceptedJob inserts a pending Work Item and an accepted job directly;
// the harness then publishes the message itself (no outbox involvement).
func (lab *experimentLab) createAcceptedJob(t *testing.T, title string) (workItemID, jobID int64) {
	t.Helper()
	ctx := context.Background()
	if err := lab.fixture.QueryRow(ctx, "INSERT INTO public.work_items (title) VALUES ($1) RETURNING id", title).Scan(&workItemID); err != nil {
		t.Fatal(err)
	}
	if err := lab.fixture.QueryRow(ctx, "INSERT INTO public.processing_jobs (work_item_id) VALUES ($1) RETURNING id", workItemID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	lab.record.observe("work_item_id", workItemID)
	lab.record.observe("job_id", jobID)
	return workItemID, jobID
}

func (lab *experimentLab) publish(t *testing.T, workItemID, jobID int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := publishFixturePayload(ctx, lab.config.RabbitFixtureURL, lab.config.Queue, workerPayload(jobID, workItemID)); err != nil {
		t.Fatal(err)
	}
}

type heldLock struct {
	conn *pgx.Conn
	pid  int32
}

func (lab *experimentLab) holdSessionLock(t *testing.T, key1 int, workItemID int64) *heldLock {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, withParams(lab.config.AdminURL, map[string]string{"application_name": "issue118-harness"}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	lock := &heldLock{conn: conn}
	if err := conn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&lock.pid); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1, hashint8($2::bigint))", key1, workItemID); err != nil {
		t.Fatal(err)
	}
	lab.record.sync("hold advisory lock", fmt.Sprintf("harness pid %d holds advisory lock (%d, item %d)", lock.pid, key1, workItemID), nil)
	return lock
}

func (lab *experimentLab) releaseSessionLock(t *testing.T, lock *heldLock, key1 int, workItemID int64) {
	t.Helper()
	var released bool
	if err := lock.conn.QueryRow(context.Background(), "SELECT pg_advisory_unlock($1, hashint8($2::bigint))", key1, workItemID).Scan(&released); err != nil || !released {
		t.Fatalf("release advisory lock: released=%t err=%v", released, err)
	}
	lab.record.sync("release advisory lock", fmt.Sprintf("harness pid %d released (%d, item %d)", lock.pid, key1, workItemID), nil)
}

// installCommitGate makes a successful completion for one Work Item block at
// COMMIT, after every write, until the harness releases the gate lock.
func (lab *experimentLab) installCommitGate(t *testing.T, workItemID int64) {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("issue118_commit_gate_%d", workItemID)
	if _, err := lab.admin.Exec(ctx, fmt.Sprintf(`CREATE OR REPLACE FUNCTION issue118.commit_gate() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN PERFORM pg_advisory_xact_lock(%d, hashint8(NEW.work_item_id)); RETURN NULL; END $$;
 CREATE CONSTRAINT TRIGGER %s AFTER INSERT ON public.work_item_results DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW WHEN (NEW.work_item_id = %d) EXECUTE FUNCTION issue118.commit_gate();`, commitGateLockKey, name, workItemID)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = lab.admin.Exec(context.Background(), fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON public.work_item_results", name))
	})
	lab.record.observe("instrumentation", "test-only deferred constraint trigger blocks COMMIT on a harness-held advisory lock")
}

func isCommitGateWait(backend backendObservation, gate *heldLock) bool {
	return backend.WaitEventType == "Lock" && backend.WaitEvent == "advisory" &&
		strings.EqualFold(strings.TrimSpace(backend.Query), "commit") && backend.XID != "" && blockedBy(backend, gate.pid)
}

func isItemLockWait(backend backendObservation, holder *heldLock) bool {
	return backend.WaitEventType == "Lock" && backend.WaitEvent == "advisory" && blockedBy(backend, holder.pid)
}

// postgresAction stops, kills, or starts the validated isolated PostgreSQL
// container after re-checking its Compose identity.
func (lab *experimentLab) postgresAction(t *testing.T, action string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	labels, err := command(ctx, "", nil, "docker", "inspect", "--format",
		`{{index .Config.Labels "com.docker.compose.project"}}/{{index .Config.Labels "com.docker.compose.service"}}`, lab.config.PostgresContainer)
	if err != nil {
		t.Fatal(err)
	}
	if labels != lab.config.PostgresProject+"/postgres" {
		t.Fatalf("refusing to %s container labelled %q", action, labels)
	}
	if action != "start" {
		if _, err := composeContainer(ctx, lab.config.PostgresProject, "postgres", "5432/tcp", lab.config.PostgresPort); err != nil {
			t.Fatalf("refusing to %s unverified container: %v", action, err)
		}
	}
	if _, err := command(ctx, "", nil, "docker", action, lab.config.PostgresContainer); err != nil {
		t.Fatal(err)
	}
	lab.record.sync("postgres "+action, "docker "+action+" on validated container of project "+lab.config.PostgresProject, nil)
	if action == "start" {
		lab.waitPostgresHealthy(t)
	}
}

// postgresStartWithoutHealthWait starts the validated container and returns
// as soon as docker start does, so callers can observe recovery directly.
func (lab *experimentLab) postgresStartWithoutHealthWait(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	labels, err := command(ctx, "", nil, "docker", "inspect", "--format",
		`{{index .Config.Labels "com.docker.compose.project"}}/{{index .Config.Labels "com.docker.compose.service"}}`, lab.config.PostgresContainer)
	if err != nil || labels != lab.config.PostgresProject+"/postgres" {
		t.Fatalf("refusing to start container labelled %q: %v", labels, err)
	}
	if _, err := command(ctx, "", nil, "docker", "start", lab.config.PostgresContainer); err != nil {
		t.Fatal(err)
	}
	lab.record.sync("postgres start", "docker start on validated container of project "+lab.config.PostgresProject, nil)
}

func (lab *experimentLab) waitPostgresHealthy(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	waitUntil(t, lab.record, "postgres healthy", "container healthy and same cluster accepts queries", func() (bool, any, error) {
		health, err := command(ctx, "", nil, "docker", "inspect", "--format", "{{.State.Health.Status}}", lab.config.PostgresContainer)
		if err != nil || health != "healthy" {
			return false, health, nil
		}
		var identifier string
		err = lab.admin.QueryRow(ctx, "SELECT system_identifier::text FROM pg_control_system()").Scan(&identifier)
		return err == nil && identifier == lab.config.SystemIdentifier, health, nil
	})
}

// rabbitAction stops or starts the validated isolated RabbitMQ container.
func (lab *experimentLab) rabbitAction(t *testing.T, action string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	labels, err := command(ctx, "", nil, "docker", "inspect", "--format",
		`{{index .Config.Labels "com.docker.compose.project"}}/{{index .Config.Labels "com.docker.compose.service"}}`, lab.config.RabbitMQContainer)
	if err != nil {
		t.Fatal(err)
	}
	if labels != lab.config.RabbitMQProject+"/rabbitmq" {
		t.Fatalf("refusing to %s container labelled %q", action, labels)
	}
	if action == "stop" {
		if _, err := composeContainer(ctx, lab.config.RabbitMQProject, "rabbitmq", "5672/tcp", lab.config.AMQPPort); err != nil {
			t.Fatalf("refusing to stop unverified container: %v", err)
		}
	}
	if _, err := command(ctx, "", nil, "docker", action, lab.config.RabbitMQContainer); err != nil {
		t.Fatal(err)
	}
	lab.record.sync("rabbitmq "+action, "docker "+action+" on validated container of project "+lab.config.RabbitMQProject, nil)
	if action == "start" {
		waitUntil(t, lab.record, "rabbitmq healthy", "container healthy and queue listable", func() (bool, any, error) {
			health, err := command(ctx, "", nil, "docker", "inspect", "--format", "{{.State.Health.Status}}", lab.config.RabbitMQContainer)
			if err != nil || health != "healthy" {
				return false, health, nil
			}
			counts, err := rabbitQueueCounts(lab.config)
			return err == nil, counts, nil
		})
	}
}

func httpJSON(t *testing.T, method, target string, body any) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, _ := json.Marshal(body)
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, target, reader)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	decoded := map[string]any{}
	_ = json.NewDecoder(response.Body).Decode(&decoded)
	return response.StatusCode, decoded
}

func jsonID(t *testing.T, body map[string]any, field string) int64 {
	t.Helper()
	value, ok := body[field].(float64)
	if !ok {
		t.Fatalf("response has no numeric %s: %v", field, body)
	}
	return int64(value)
}
