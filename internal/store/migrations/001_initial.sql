PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;

CREATE TABLE IF NOT EXISTS jobs (
    id              TEXT PRIMARY KEY,
    name            TEXT UNIQUE NOT NULL,
    schedule        TEXT NOT NULL,
    image           TEXT NOT NULL,
    command         TEXT NOT NULL,
    cpu_limit       TEXT NOT NULL DEFAULT '',
    memory_limit    TEXT NOT NULL DEFAULT '',
    timeout_seconds INTEGER NOT NULL DEFAULT 300,
    enabled         BOOLEAN NOT NULL DEFAULT 1,
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS job_runs (
    id            TEXT PRIMARY KEY,
    job_id        TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    started_at    DATETIME NOT NULL,
    finished_at   DATETIME,
    status        TEXT NOT NULL,
    exit_code     INTEGER NOT NULL DEFAULT 0,
    container_id  TEXT NOT NULL DEFAULT '',
    log_output    TEXT NOT NULL DEFAULT '',
    error_message TEXT
);

CREATE INDEX IF NOT EXISTS idx_job_runs_job_id ON job_runs(job_id);
CREATE INDEX IF NOT EXISTS idx_job_runs_started_at ON job_runs(started_at DESC);
