package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"github.com/yourusername/saturn/internal/executor"
	"github.com/yourusername/saturn/internal/logger"
	"github.com/yourusername/saturn/internal/store"
)

const syncInterval = 15 * time.Second

// cronEntry tracks a registered cron entry alongside the schedule it was
// registered with, so we can detect schedule changes.
type cronEntry struct {
	id       cron.EntryID
	schedule string
}

// Scheduler dispatches Jobs on their cron schedule using a Docker executor.
type Scheduler struct {
	cron     *cron.Cron
	store    store.Store
	executor executor.Executor
	logger   *slog.Logger

	mu          sync.Mutex
	cronEntries map[string]cronEntry // keyed by job ID

	inFlight sync.Map
	wg       sync.WaitGroup
	stopSync chan struct{}
}

// New creates a Scheduler wired to the given store and executor.
func New(st store.Store, exec executor.Executor, log *slog.Logger) *Scheduler {
	return &Scheduler{
		cron:        cron.New(cron.WithLogger(cron.DiscardLogger)),
		store:       st,
		executor:    exec,
		logger:      log,
		cronEntries: make(map[string]cronEntry),
		stopSync:    make(chan struct{}),
	}
}

// Start performs an initial job sync, starts the cron runner, then launches a
// background goroutine that re-syncs jobs every syncInterval. It does not block.
func (s *Scheduler) Start(ctx context.Context) error {
	if err := s.syncJobs(ctx); err != nil {
		return err
	}
	s.cron.Start()
	go s.syncLoop(ctx)
	return nil
}

// Stop halts the sync loop, the cron runner, and waits for in-flight jobs.
func (s *Scheduler) Stop() {
	close(s.stopSync)
	s.cron.Stop()
	s.wg.Wait()
	s.logger.Info("scheduler stopped")
}

// syncLoop re-syncs jobs on a fixed interval until Stop is called.
func (s *Scheduler) syncLoop(ctx context.Context) {
	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := s.syncJobs(ctx); err != nil {
				s.logger.Error("job sync failed", "error", err)
			}
		case <-s.stopSync:
			return
		}
	}
}

// syncJobs diffs the store against registered cron entries:
//   - newly enabled jobs are registered
//   - jobs whose schedule changed are re-registered
//   - disabled or deleted jobs are removed
func (s *Scheduler) syncJobs(ctx context.Context) error {
	jobs, err := s.store.ListJobs(ctx)
	if err != nil {
		return err
	}

	enabled := make(map[string]store.Job, len(jobs))
	for _, j := range jobs {
		if j.Enabled {
			enabled[j.ID] = j
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove entries for jobs that are disabled, deleted, or rescheduled.
	for jobID, entry := range s.cronEntries {
		j, ok := enabled[jobID]
		if !ok || j.Schedule != entry.schedule {
			s.cron.Remove(entry.id)
			delete(s.cronEntries, jobID)
			if ok {
				s.logger.Info("job rescheduled, re-registering", "job_name", j.Name)
			}
		}
	}

	// Add entries for jobs not yet registered.
	added := 0
	for jobID, j := range enabled {
		if _, ok := s.cronEntries[jobID]; ok {
			continue
		}
		j := j
		id, err := s.cron.AddFunc(j.Schedule, func() {
			s.dispatchJob(ctx, j)
		})
		if err != nil {
			s.logger.Error("failed to register job", "job_name", j.Name, "error", err)
			continue
		}
		s.cronEntries[jobID] = cronEntry{id: id, schedule: j.Schedule}
		added++
	}

	if added > 0 {
		s.logger.Info("scheduler synced", "registered", added, "total", len(s.cronEntries))
	}
	return nil
}

// dispatchJob checks whether a job is already running; if not, it spawns a
// goroutine to execute it. This is the core dispatch function and is
// intentionally package-private so tests can call it directly.
func (s *Scheduler) dispatchJob(ctx context.Context, job store.Job) {
	// Re-fetch to pick up any field changes since registration.
	current, err := s.store.GetJob(ctx, job.Name)
	if err != nil {
		s.logger.Error("dispatch: failed to re-fetch job",
			"job_name", job.Name, "error", err)
		return
	}
	if !current.Enabled {
		return
	}

	if _, loaded := s.inFlight.LoadOrStore(current.ID, struct{}{}); loaded {
		s.logger.Warn("job already running, skipping",
			"job_name", current.Name,
			"job_id", current.ID,
			"event", "job.skipped",
		)
		return
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.inFlight.Delete(current.ID)
		s.runJob(ctx, *current)
	}()
}

func (s *Scheduler) runJob(ctx context.Context, job store.Job) {
	runID := uuid.New().String()
	log := logger.WithJob(s.logger, job.Name, job.ID, runID)

	now := time.Now()
	run := store.JobRun{
		ID:        runID,
		JobID:     job.ID,
		StartedAt: now,
		Status:    "running",
	}
	if err := s.store.CreateRun(ctx, run); err != nil {
		log.Error("failed to create run record", "error", err)
		return
	}
	log.Info("job starting", "event", "job.start")

	jobCtx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(job.TimeoutSeconds)*time.Second,
	)
	defer cancel()

	result := s.executor.Run(jobCtx, executor.RunOptions{
		Image:          job.Image,
		Command:        job.Command,
		Volumes:        job.Volumes,
		CPULimit:       job.CPULimit,
		MemoryLimit:    job.MemoryLimit,
		TimeoutSeconds: job.TimeoutSeconds,
	})

	finished := time.Now()
	run.FinishedAt = &finished
	run.ContainerID = result.ContainerID
	run.LogOutput = result.LogOutput
	run.ExitCode = result.ExitCode

	var event string
	switch {
	case result.Error == context.DeadlineExceeded || result.Error == context.Canceled:
		run.Status = "timeout"
		event = "job.timeout"
		msg := result.Error.Error()
		run.ErrorMessage = &msg
	case result.Error != nil:
		run.Status = "failed"
		event = "job.failed"
		msg := result.Error.Error()
		run.ErrorMessage = &msg
	case result.ExitCode != 0:
		run.Status = "failed"
		event = "job.failed"
	default:
		run.Status = "success"
		event = "job.success"
	}

	if err := s.store.UpdateRun(ctx, run); err != nil {
		log.Error("failed to update run record", "error", err)
	}
	log.Info("job finished",
		"event", event,
		"status", run.Status,
		"exit_code", run.ExitCode,
	)
}
