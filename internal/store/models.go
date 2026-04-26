package store

import "time"

// Job represents a registered cronjob.
type Job struct {
	ID             string
	Name           string
	Schedule       string
	Image          string
	Command        []string
	Volumes        []string // bind mounts in "host:container" format
	CPULimit       string
	MemoryLimit    string
	TimeoutSeconds int
	Enabled        bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// JobRun represents a single execution of a Job.
type JobRun struct {
	ID           string
	JobID        string
	StartedAt    time.Time
	FinishedAt   *time.Time
	Status       string // "running" | "success" | "failed" | "timeout"
	ExitCode     int
	ContainerID  string
	LogOutput    string
	ErrorMessage *string
}
