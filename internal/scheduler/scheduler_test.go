package scheduler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/yourusername/saturn/internal/executor"
	"github.com/yourusername/saturn/internal/store"
)

// ── mock Store ────────────────────────────────────────────────────────────────

type mockStore struct {
	mu   sync.Mutex
	jobs map[string]*store.Job // keyed by name
	runs []store.JobRun

	getJobErr error
}

func newMockStore(jobs ...store.Job) *mockStore {
	m := &mockStore{jobs: make(map[string]*store.Job)}
	for i := range jobs {
		j := jobs[i]
		m.jobs[j.Name] = &j
	}
	return m
}

func (m *mockStore) GetJob(_ context.Context, name string) (*store.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getJobErr != nil {
		return nil, m.getJobErr
	}
	if j, ok := m.jobs[name]; ok {
		cp := *j
		return &cp, nil
	}
	return nil, fmt.Errorf("job %q not found", name)
}
func (m *mockStore) CreateJob(_ context.Context, j store.Job) error { return nil }
func (m *mockStore) ListJobs(_ context.Context) ([]store.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Job
	for _, j := range m.jobs {
		out = append(out, *j)
	}
	return out, nil
}
func (m *mockStore) UpdateJob(_ context.Context, j store.Job) error { return nil }
func (m *mockStore) DeleteJob(_ context.Context, name string) error { return nil }

func (m *mockStore) CreateRun(_ context.Context, r store.JobRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs = append(m.runs, r)
	return nil
}
func (m *mockStore) UpdateRun(_ context.Context, r store.JobRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, existing := range m.runs {
		if existing.ID == r.ID {
			m.runs[i] = r
			return nil
		}
	}
	return fmt.Errorf("run %q not found", r.ID)
}
func (m *mockStore) ListRuns(_ context.Context, jobID string, limit int) ([]store.JobRun, error) {
	return nil, nil
}

func (m *mockStore) runCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.runs)
}

func (m *mockStore) latestRun() (store.JobRun, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.runs) == 0 {
		return store.JobRun{}, false
	}
	return m.runs[len(m.runs)-1], true
}

// ── mock Executor ─────────────────────────────────────────────────────────────

type mockExecutor struct {
	mu        sync.Mutex
	callCount int
	result    executor.RunResult
	blockCh   chan struct{} // if non-nil, Run blocks until closed
}

func (m *mockExecutor) Run(ctx context.Context, _ executor.RunOptions) executor.RunResult {
	m.mu.Lock()
	m.callCount++
	m.mu.Unlock()

	if m.blockCh != nil {
		select {
		case <-m.blockCh:
		case <-ctx.Done():
			return executor.RunResult{Error: ctx.Err(), ExitCode: -1}
		}
	}
	return m.result
}

func (m *mockExecutor) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// ── helpers ───────────────────────────────────────────────────────────────────

func newTestScheduler(st store.Store, exec executor.Executor) *Scheduler {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(st, exec, log)
	s.cron = cron.New(cron.WithLogger(cron.DiscardLogger))
	return s
}

func testJob() store.Job {
	return store.Job{
		ID:             "job-test-1",
		Name:           "test-job",
		Schedule:       "* * * * *",
		Image:          "alpine:latest",
		Command:        []string{"echo", "hello"},
		TimeoutSeconds: 30,
		Enabled:        true,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestDispatchJob_ExecutorIsCalled verifies that dispatching a job calls the
// executor and records a run with appropriate status.
func TestDispatchJob_ExecutorIsCalled(t *testing.T) {
	job := testJob()
	st := newMockStore(job)
	exec := &mockExecutor{result: executor.RunResult{ExitCode: 0}}
	sched := newTestScheduler(st, exec)

	sched.dispatchJob(context.Background(), job)
	sched.wg.Wait()

	if exec.calls() != 1 {
		t.Fatalf("expected executor called once, got %d", exec.calls())
	}
	if st.runCount() != 1 {
		t.Fatalf("expected 1 run record, got %d", st.runCount())
	}
	run, _ := st.latestRun()
	if run.Status != "success" {
		t.Errorf("expected status=success, got %q", run.Status)
	}
	if run.ExitCode != 0 {
		t.Errorf("expected exit_code=0, got %d", run.ExitCode)
	}
}

// TestDispatchJob_NonZeroExitMarkedFailed verifies that a non-zero exit code
// results in status "failed".
func TestDispatchJob_NonZeroExitMarkedFailed(t *testing.T) {
	job := testJob()
	st := newMockStore(job)
	exec := &mockExecutor{result: executor.RunResult{ExitCode: 1}}
	sched := newTestScheduler(st, exec)

	sched.dispatchJob(context.Background(), job)
	sched.wg.Wait()

	run, ok := st.latestRun()
	if !ok {
		t.Fatal("no run recorded")
	}
	if run.Status != "failed" {
		t.Errorf("expected status=failed, got %q", run.Status)
	}
}

// TestDispatchJob_SkipsIfAlreadyRunning verifies that a second dispatch is
// dropped when the first is still in-flight.
func TestDispatchJob_SkipsIfAlreadyRunning(t *testing.T) {
	job := testJob()
	st := newMockStore(job)

	blockCh := make(chan struct{})
	exec := &mockExecutor{
		blockCh: blockCh,
		result:  executor.RunResult{ExitCode: 0},
	}
	sched := newTestScheduler(st, exec)

	// First dispatch — will block inside executor.Run until we close blockCh.
	sched.dispatchJob(context.Background(), job)

	// Give the goroutine time to mark itself in-flight.
	time.Sleep(20 * time.Millisecond)

	// Second dispatch — should be skipped because job is in-flight.
	sched.dispatchJob(context.Background(), job)

	// Unblock the first execution.
	close(blockCh)
	sched.wg.Wait()

	if exec.calls() != 1 {
		t.Errorf("executor should be called exactly once (skip), got %d", exec.calls())
	}
}

// TestDispatchJob_DisabledJobIsSkipped verifies that a disabled job is not run.
func TestDispatchJob_DisabledJobIsSkipped(t *testing.T) {
	job := testJob()
	job.Enabled = false
	st := newMockStore(job)
	exec := &mockExecutor{result: executor.RunResult{ExitCode: 0}}
	sched := newTestScheduler(st, exec)

	sched.dispatchJob(context.Background(), job)
	sched.wg.Wait()

	if exec.calls() != 0 {
		t.Errorf("executor should not be called for disabled job, got %d", exec.calls())
	}
	if st.runCount() != 0 {
		t.Errorf("no run should be recorded for disabled job, got %d", st.runCount())
	}
}

// TestDispatchJob_TimeoutStatus verifies that context cancellation marks the
// run as "timeout".
func TestDispatchJob_TimeoutStatus(t *testing.T) {
	job := testJob()
	st := newMockStore(job)

	blockCh := make(chan struct{}) // never closed; relies on context cancel
	exec := &mockExecutor{
		blockCh: blockCh,
		result:  executor.RunResult{Error: context.DeadlineExceeded, ExitCode: -1},
	}
	sched := newTestScheduler(st, exec)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	sched.dispatchJob(ctx, job)
	sched.wg.Wait()

	run, ok := st.latestRun()
	if !ok {
		t.Fatal("no run recorded")
	}
	if run.Status != "timeout" {
		t.Errorf("expected status=timeout, got %q", run.Status)
	}
}

// TestStart_RegistersEnabledJobsOnly verifies that Start only schedules
// enabled jobs from the store.
func TestStart_RegistersEnabledJobsOnly(t *testing.T) {
	enabled := testJob()
	disabled := testJob()
	disabled.ID = "job-disabled"
	disabled.Name = "disabled-job"
	disabled.Enabled = false

	st := newMockStore(enabled, disabled)
	exec := &mockExecutor{result: executor.RunResult{ExitCode: 0}}
	sched := newTestScheduler(st, exec)

	if err := sched.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sched.Stop()

	entries := sched.cron.Entries()
	if len(entries) != 1 {
		t.Errorf("expected 1 cron entry (enabled only), got %d", len(entries))
	}
}
