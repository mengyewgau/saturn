package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// SQLiteStore is a Store backed by a SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) the SQLite database at dbPath and runs migrations.
// Pass ":memory:" for an in-memory database (useful for tests).
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	dsn := dbPath
	if dbPath == ":memory:" {
		dsn = "file::memory:?cache=shared"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// SQLite performs best with a single writer connection.
	db.SetMaxOpenConns(1)

	s := &SQLiteStore{db: db}
	if err := s.runMigrations(); err != nil {
		db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}
	return s, nil
}

// Close releases the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) runMigrations() error {
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name       TEXT PRIMARY KEY,
			applied_at DATETIME NOT NULL
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		name := e.Name()

		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name=?`, name).Scan(&count); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if count > 0 {
			continue
		}

		data, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := s.db.Exec(string(data)); err != nil {
			if !isIdempotentErr(err) {
				return fmt.Errorf("apply migration %s: %w", name, err)
			}
			// Schema already contains this change; record it and move on.
		}
		if _, err := s.db.Exec(`INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`,
			name, time.Now().Format(time.RFC3339)); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
	}
	return nil
}

// isIdempotentErr returns true for SQLite errors that mean the schema change
// was already applied (duplicate column, table already exists, etc.).
func isIdempotentErr(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "duplicate column name") ||
		strings.Contains(msg, "already exists")
}

// CreateJob inserts a new job record.
func (s *SQLiteStore) CreateJob(ctx context.Context, job Job) error {
	cmdJSON, err := json.Marshal(job.Command)
	if err != nil {
		return fmt.Errorf("marshal command: %w", err)
	}
	volJSON, err := json.Marshal(job.Volumes)
	if err != nil {
		return fmt.Errorf("marshal volumes: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO jobs
			(id, name, schedule, image, command, volumes, cpu_limit, memory_limit, timeout_seconds, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Name, job.Schedule, job.Image, string(cmdJSON), string(volJSON),
		job.CPULimit, job.MemoryLimit, job.TimeoutSeconds,
		boolToInt(job.Enabled),
		job.CreatedAt.Format(time.RFC3339),
		job.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert job: %w", err)
	}
	return nil
}

// GetJob fetches a job by name.
func (s *SQLiteStore) GetJob(ctx context.Context, name string) (*Job, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, schedule, image, command, volumes, cpu_limit, memory_limit,
		       timeout_seconds, enabled, created_at, updated_at
		FROM jobs WHERE name = ?`, name)
	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("job %q not found", name)
	}
	if err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}
	return job, nil
}

// ListJobs returns all jobs ordered by name.
func (s *SQLiteStore) ListJobs(ctx context.Context) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, schedule, image, command, volumes, cpu_limit, memory_limit,
		       timeout_seconds, enabled, created_at, updated_at
		FROM jobs ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		jobs = append(jobs, *job)
	}
	return jobs, rows.Err()
}

// UpdateJob updates all mutable fields of a job identified by its ID.
func (s *SQLiteStore) UpdateJob(ctx context.Context, job Job) error {
	cmdJSON, err := json.Marshal(job.Command)
	if err != nil {
		return fmt.Errorf("marshal command: %w", err)
	}
	volJSON, err := json.Marshal(job.Volumes)
	if err != nil {
		return fmt.Errorf("marshal volumes: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE jobs
		SET name=?, schedule=?, image=?, command=?, volumes=?, cpu_limit=?, memory_limit=?,
		    timeout_seconds=?, enabled=?, updated_at=?
		WHERE id=?`,
		job.Name, job.Schedule, job.Image, string(cmdJSON), string(volJSON),
		job.CPULimit, job.MemoryLimit, job.TimeoutSeconds,
		boolToInt(job.Enabled),
		job.UpdatedAt.Format(time.RFC3339),
		job.ID,
	)
	if err != nil {
		return fmt.Errorf("update job: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("job %q not found", job.Name)
	}
	return nil
}

// DeleteJob removes a job and all its runs by name.
func (s *SQLiteStore) DeleteJob(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM jobs WHERE name=?`, name)
	if err != nil {
		return fmt.Errorf("delete job: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("job %q not found", name)
	}
	return nil
}

// CreateRun inserts a new job run record.
func (s *SQLiteStore) CreateRun(ctx context.Context, run JobRun) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO job_runs
			(id, job_id, started_at, finished_at, status, exit_code, container_id, log_output, error_message)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.JobID,
		run.StartedAt.Format(time.RFC3339),
		nullableTime(run.FinishedAt),
		run.Status, run.ExitCode, run.ContainerID, run.LogOutput,
		nullableString(run.ErrorMessage),
	)
	if err != nil {
		return fmt.Errorf("insert run: %w", err)
	}
	return nil
}

// UpdateRun updates a job run record.
func (s *SQLiteStore) UpdateRun(ctx context.Context, run JobRun) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE job_runs
		SET finished_at=?, status=?, exit_code=?, container_id=?, log_output=?, error_message=?
		WHERE id=?`,
		nullableTime(run.FinishedAt),
		run.Status, run.ExitCode, run.ContainerID, run.LogOutput,
		nullableString(run.ErrorMessage),
		run.ID,
	)
	if err != nil {
		return fmt.Errorf("update run: %w", err)
	}
	return nil
}

// ListRuns returns the most recent runs for a job, newest first.
func (s *SQLiteStore) ListRuns(ctx context.Context, jobID string, limit int) ([]JobRun, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, job_id, started_at, finished_at, status, exit_code, container_id, log_output, error_message
		FROM job_runs
		WHERE job_id = ?
		ORDER BY started_at DESC
		LIMIT ?`, jobID, limit)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()

	var runs []JobRun
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		runs = append(runs, *run)
	}
	return runs, rows.Err()
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanJob(s scanner) (*Job, error) {
	var (
		job          Job
		cmdJSON      string
		volJSON      string
		createdAtStr string
		updatedAtStr string
		enabledInt   int
	)
	err := s.Scan(
		&job.ID, &job.Name, &job.Schedule, &job.Image,
		&cmdJSON, &volJSON, &job.CPULimit, &job.MemoryLimit,
		&job.TimeoutSeconds, &enabledInt,
		&createdAtStr, &updatedAtStr,
	)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(cmdJSON), &job.Command); err != nil {
		return nil, fmt.Errorf("unmarshal command: %w", err)
	}
	if err := json.Unmarshal([]byte(volJSON), &job.Volumes); err != nil {
		return nil, fmt.Errorf("unmarshal volumes: %w", err)
	}
	job.Enabled = enabledInt != 0
	job.CreatedAt, err = time.Parse(time.RFC3339, createdAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	job.UpdatedAt, err = time.Parse(time.RFC3339, updatedAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}
	return &job, nil
}

func scanRun(s scanner) (*JobRun, error) {
	var (
		run          JobRun
		startedAtStr string
		finishedAt   sql.NullString
		errorMsg     sql.NullString
	)
	err := s.Scan(
		&run.ID, &run.JobID,
		&startedAtStr, &finishedAt,
		&run.Status, &run.ExitCode, &run.ContainerID, &run.LogOutput,
		&errorMsg,
	)
	if err != nil {
		return nil, err
	}
	run.StartedAt, err = time.Parse(time.RFC3339, startedAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse started_at: %w", err)
	}
	if finishedAt.Valid {
		t, err := time.Parse(time.RFC3339, finishedAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse finished_at: %w", err)
		}
		run.FinishedAt = &t
	}
	if errorMsg.Valid {
		run.ErrorMessage = &errorMsg.String
	}
	return &run, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullableString(s *string) interface{} {
	if s == nil {
		return nil
	}
	return *s
}

func nullableTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.Format(time.RFC3339)
}
