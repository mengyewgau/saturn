package executor

import "context"

// RunOptions configures a container execution.
type RunOptions struct {
	Image          string
	Command        []string
	Volumes        []string // bind mounts in "host:container" format
	CPULimit       string
	MemoryLimit    string
	TimeoutSeconds int
}

// RunResult holds the outcome of a container execution.
type RunResult struct {
	ExitCode    int
	LogOutput   string
	ContainerID string
	Error       error
}

// Executor runs a container and returns its result.
type Executor interface {
	Run(ctx context.Context, opts RunOptions) RunResult
}
