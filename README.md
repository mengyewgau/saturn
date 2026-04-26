# Saturn

A CLI cron job manager backed by Docker and SQLite.

Saturn lets you register scheduled jobs that run as Docker containers, then manages their execution, logging, and lifecycle from a single persistent daemon.

---

## How it works

1. **Register jobs** with `saturn job add` — each job specifies a Docker image, a cron schedule, and optional resource limits.
2. **Start the daemon** with `saturn run` — it polls the job store every 15 seconds and fires containers on schedule.
3. **Inspect results** with `saturn logs` and `saturn status` — run history, exit codes, and output are stored in a local SQLite database at `~/.saturn/saturn.db`.

---

## Installation

```bash
make build        # produces bin/saturn
make install      # copies bin/saturn to /usr/local/bin
```

Requires Docker and Go 1.21+.

---

## Quick reference

| Command | Description |
|---|---|
| `saturn run` | Start the scheduler daemon |
| `saturn job add` | Register a new job |
| `saturn job list` | List all registered jobs |
| `saturn job update <name>` | Update job fields |
| `saturn job remove <name>` | Remove a job |
| `saturn job enable <name>` | Enable a disabled job |
| `saturn job disable <name>` | Disable a job without removing it |
| `saturn logs <name>` | Show execution history for a job |
| `saturn status` | Show daemon state and job health |

---

## Example

```bash
# Register a job that runs a Python script every hour
saturn job add \
  --name my-script \
  --schedule "0 * * * *" \
  --image python:3.12-slim \
  --command python --command /work/script.py \
  --volume /home/user/scripts:/work \
  --memory-limit 128m \
  --timeout 120

# Start the daemon
saturn run

# Check on it
saturn status
saturn logs my-script --last 5
```

---

## Creating a quickstart

The `quickstart/` directory is a good place to keep self-contained job definitions. Here is how to go from an idea to a running job.

### 1. Pick a Docker image

Choose an image that has everything your job needs. For a Python script use `python:3.12-slim`; for a shell one-liner use `alpine:latest`. Keep images small — they are pulled fresh if not cached locally.

### 2. Write your script (optional)

If your job is more than a one-liner, put the script file inside a subdirectory of `quickstart/`:

```
quickstart/
  my-job/
    script.py
    config.json   # any config your script needs
```

### 3. Register the job

Mount your script directory into the container with `--volume` and point the command at it:

```bash
saturn job add \
  --name my-job \
  --schedule "0 * * * *" \
  --image python:3.12-slim \
  --command python --command /work/script.py \
  --volume /absolute/path/to/quickstart/my-job:/work \
  --memory-limit 128m \
  --timeout 60
```

Common schedule expressions:

| Expression | Meaning |
|---|---|
| `* * * * *` | Every minute |
| `*/15 * * * *` | Every 15 minutes |
| `0 * * * *` | Every hour |
| `0 9 * * 1-5` | Weekdays at 09:00 |
| `0 0 * * *` | Daily at midnight |

### 4. Start the daemon and verify

```bash
saturn run &          # start in background
saturn status         # confirm daemon is running and job is registered
saturn logs my-job    # inspect output after the first run fires
```

To stop the daemon, send it SIGTERM (`kill <pid>`) or press Ctrl-C if running in the foreground.

---

## State directory

Saturn stores everything under `~/.saturn/`:

| File | Purpose |
|---|---|
| `saturn.db` | SQLite store — jobs and run history |
| `saturn.log` | Daemon log |
| `saturn.pid` | PID of the running daemon |
