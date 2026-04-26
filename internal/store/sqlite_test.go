package store

import (
	"context"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	st, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func baseJob() Job {
	now := time.Now().Truncate(time.Second).UTC()
	return Job{
		ID:             "job-001",
		Name:           "backup",
		Schedule:       "*/5 * * * *",
		Image:          "alpine:latest",
		Command:        []string{"/bin/sh", "-c", "echo hello"},
		CPULimit:       "0.5",
		MemoryLimit:    "128m",
		TimeoutSeconds: 300,
		Enabled:        true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// ── CreateJob / GetJob ────────────────────────────────────────────────────────

func TestCreateAndGetJob(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	want := baseJob()

	if err := st.CreateJob(ctx, want); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	got, err := st.GetJob(ctx, want.Name)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	assertJobEqual(t, want, *got)
}

func TestGetJob_NotFound(t *testing.T) {
	st := newTestStore(t)
	_, err := st.GetJob(context.Background(), "no-such-job")
	if err == nil {
		t.Fatal("expected error for missing job, got nil")
	}
}

func TestCreateJob_DuplicateName(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	j := baseJob()
	if err := st.CreateJob(ctx, j); err != nil {
		t.Fatalf("first CreateJob: %v", err)
	}
	j.ID = "job-002"
	if err := st.CreateJob(ctx, j); err == nil {
		t.Fatal("expected duplicate-name error, got nil")
	}
}

// ── ListJobs ──────────────────────────────────────────────────────────────────

func TestListJobs(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	jobs := []Job{
		{ID: "1", Name: "aaa", Schedule: "* * * * *", Image: "alpine", Command: []string{"echo"},
			TimeoutSeconds: 60, Enabled: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()},
		{ID: "2", Name: "bbb", Schedule: "* * * * *", Image: "alpine", Command: []string{"echo"},
			TimeoutSeconds: 60, Enabled: false, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()},
	}
	for _, j := range jobs {
		if err := st.CreateJob(ctx, j); err != nil {
			t.Fatalf("CreateJob %q: %v", j.Name, err)
		}
	}

	got, err := st.ListJobs(ctx)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 jobs, got %d", len(got))
	}
	// Should be sorted by name.
	if got[0].Name != "aaa" || got[1].Name != "bbb" {
		t.Fatalf("unexpected order: %v", []string{got[0].Name, got[1].Name})
	}
}

// ── UpdateJob ─────────────────────────────────────────────────────────────────

func TestUpdateJob(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	j := baseJob()
	if err := st.CreateJob(ctx, j); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	j.Enabled = false
	j.CPULimit = "1.0"
	j.UpdatedAt = time.Now().Truncate(time.Second).UTC()
	if err := st.UpdateJob(ctx, j); err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}

	got, err := st.GetJob(ctx, j.Name)
	if err != nil {
		t.Fatalf("GetJob after update: %v", err)
	}
	if got.Enabled {
		t.Error("expected Enabled=false after update")
	}
	if got.CPULimit != "1.0" {
		t.Errorf("CPULimit: want 1.0, got %q", got.CPULimit)
	}
}

// ── DeleteJob ─────────────────────────────────────────────────────────────────

func TestDeleteJob(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	j := baseJob()
	if err := st.CreateJob(ctx, j); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if err := st.DeleteJob(ctx, j.Name); err != nil {
		t.Fatalf("DeleteJob: %v", err)
	}
	if _, err := st.GetJob(ctx, j.Name); err == nil {
		t.Fatal("expected error after DeleteJob, got nil")
	}
}

func TestDeleteJob_NotFound(t *testing.T) {
	st := newTestStore(t)
	if err := st.DeleteJob(context.Background(), "ghost"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── CreateRun / ListRuns ──────────────────────────────────────────────────────

func TestCreateAndListRuns(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	j := baseJob()
	if err := st.CreateJob(ctx, j); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	runs := []JobRun{
		{ID: "r1", JobID: j.ID, StartedAt: time.Now().Add(-2 * time.Minute).UTC(), Status: "success", ExitCode: 0},
		{ID: "r2", JobID: j.ID, StartedAt: time.Now().Add(-1 * time.Minute).UTC(), Status: "failed", ExitCode: 1},
	}
	for _, r := range runs {
		if err := st.CreateRun(ctx, r); err != nil {
			t.Fatalf("CreateRun %q: %v", r.ID, err)
		}
	}

	got, err := st.ListRuns(ctx, j.ID, 10)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 runs, got %d", len(got))
	}
	// ListRuns returns newest first.
	if got[0].ID != "r2" {
		t.Errorf("want newest run first (r2), got %s", got[0].ID)
	}
}

func TestListRuns_Limit(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	j := baseJob()
	if err := st.CreateJob(ctx, j); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	for i := 0; i < 5; i++ {
		r := JobRun{
			ID:        "r" + string(rune('0'+i)),
			JobID:     j.ID,
			StartedAt: time.Now().UTC(),
			Status:    "success",
		}
		if err := st.CreateRun(ctx, r); err != nil {
			t.Fatalf("CreateRun: %v", err)
		}
	}
	got, err := st.ListRuns(ctx, j.ID, 3)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 runs with limit=3, got %d", len(got))
	}
}

// ── UpdateRun ─────────────────────────────────────────────────────────────────

func TestUpdateRun(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	j := baseJob()
	if err := st.CreateJob(ctx, j); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	run := JobRun{
		ID:        "run-1",
		JobID:     j.ID,
		StartedAt: time.Now().UTC(),
		Status:    "running",
	}
	if err := st.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	finished := time.Now().UTC()
	run.Status = "success"
	run.ExitCode = 0
	run.FinishedAt = &finished
	run.LogOutput = "hello world"
	if err := st.UpdateRun(ctx, run); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}

	got, err := st.ListRuns(ctx, j.ID, 1)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if got[0].Status != "success" {
		t.Errorf("want status=success, got %q", got[0].Status)
	}
	if got[0].LogOutput != "hello world" {
		t.Errorf("want log_output=%q, got %q", "hello world", got[0].LogOutput)
	}
	if got[0].FinishedAt == nil {
		t.Error("expected FinishedAt to be set")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func assertJobEqual(t *testing.T, want, got Job) {
	t.Helper()
	if want.ID != got.ID {
		t.Errorf("ID: want %q, got %q", want.ID, got.ID)
	}
	if want.Name != got.Name {
		t.Errorf("Name: want %q, got %q", want.Name, got.Name)
	}
	if want.Schedule != got.Schedule {
		t.Errorf("Schedule: want %q, got %q", want.Schedule, got.Schedule)
	}
	if want.Image != got.Image {
		t.Errorf("Image: want %q, got %q", want.Image, got.Image)
	}
	if want.Enabled != got.Enabled {
		t.Errorf("Enabled: want %v, got %v", want.Enabled, got.Enabled)
	}
	if want.TimeoutSeconds != got.TimeoutSeconds {
		t.Errorf("TimeoutSeconds: want %d, got %d", want.TimeoutSeconds, got.TimeoutSeconds)
	}
	if len(want.Command) != len(got.Command) {
		t.Errorf("Command len: want %d, got %d", len(want.Command), len(got.Command))
	} else {
		for i := range want.Command {
			if want.Command[i] != got.Command[i] {
				t.Errorf("Command[%d]: want %q, got %q", i, want.Command[i], got.Command[i])
			}
		}
	}
}
