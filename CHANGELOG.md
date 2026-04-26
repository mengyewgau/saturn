# Changelog

All notable changes to this project will be documented here.

## [1.0.0] - 2026-04-26

### Added

- `saturn run` — scheduler daemon that polls the job store and fires Docker containers on schedule
- `saturn job add` — register a new cron job with image, schedule, volumes, and resource limits
- `saturn job update` — update one or more fields of an existing job
- `saturn job remove` — remove a job by name
- `saturn job list` — list all registered jobs in a tabular view
- `saturn job enable` / `saturn job disable` — toggle a job without removing it
- `saturn logs` — display execution history and output for a job
- `saturn status` — show daemon state, database path, and a per-job health summary
- SQLite-backed persistent store at `~/.saturn/saturn.db`
- Docker executor with CPU and memory limit support
- Execution timeout per job (default 300 s)
- PID file at `~/.saturn/saturn.pid` for daemon detection
- Structured log file at `~/.saturn/saturn.log`
- `quickstart/` directory with example jobs
