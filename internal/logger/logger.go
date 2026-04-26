package logger

import (
	"io"
	"log/slog"
	"os"
)

// New creates a JSON slog.Logger that writes to both stdout and logPath.
func New(logPath string) *slog.Logger {
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	w := io.MultiWriter(os.Stdout, f)
	return slog.New(slog.NewJSONHandler(w, nil))
}

// WithJob returns a child logger pre-populated with job context fields.
func WithJob(l *slog.Logger, jobName, jobID, runID string) *slog.Logger {
	return l.With(
		"job_name", jobName,
		"job_id", jobID,
		"run_id", runID,
	)
}
