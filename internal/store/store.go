package store

import "context"

// Store defines the persistence interface for crond.
type Store interface {
	CreateJob(ctx context.Context, job Job) error
	GetJob(ctx context.Context, name string) (*Job, error)
	ListJobs(ctx context.Context) ([]Job, error)
	UpdateJob(ctx context.Context, job Job) error
	DeleteJob(ctx context.Context, name string) error

	CreateRun(ctx context.Context, run JobRun) error
	UpdateRun(ctx context.Context, run JobRun) error
	ListRuns(ctx context.Context, jobID string, limit int) ([]JobRun, error)
}
