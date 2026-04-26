package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"github.com/spf13/cobra"
	"github.com/yourusername/saturn/internal/store"
)

var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manage cronjobs",
}

// ── job add ──────────────────────────────────────────────────────────────────

var jobAddFlags struct {
	name           string
	schedule       string
	image          string
	command        []string
	volumes        []string
	cpuLimit       string
	memoryLimit    string
	timeoutSeconds int
}

var jobAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Register a new cronjob",
	RunE:  runJobAdd,
}

// ── job update ────────────────────────────────────────────────────────────────

var jobUpdateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Update one or more fields of an existing job",
	Args:  cobra.ExactArgs(1),
	RunE:  runJobUpdate,
}

func init() {
	// job add flags
	f := jobAddCmd.Flags()
	f.StringVar(&jobAddFlags.name, "name", "", "Unique job name (required)")
	f.StringVar(&jobAddFlags.schedule, "schedule", "", "Cron schedule expression, e.g. \"*/5 * * * *\" (required)")
	f.StringVar(&jobAddFlags.image, "image", "", "Docker image to run (required)")
	f.StringArrayVar(&jobAddFlags.command, "command", nil, "Container command; repeat flag for each argument")
	f.StringArrayVar(&jobAddFlags.volumes, "volume", nil, "Bind mount in host:container format; repeat flag for each mount")
	f.StringVar(&jobAddFlags.cpuLimit, "cpu-limit", "", "CPU limit, e.g. \"0.5\"")
	f.StringVar(&jobAddFlags.memoryLimit, "memory-limit", "", "Memory limit, e.g. \"128m\"")
	f.IntVar(&jobAddFlags.timeoutSeconds, "timeout", 300, "Execution timeout in seconds")

	jobAddCmd.MarkFlagRequired("name")
	jobAddCmd.MarkFlagRequired("schedule")
	jobAddCmd.MarkFlagRequired("image")

	// job update flags
	u := jobUpdateCmd.Flags()
	u.String("schedule", "", "New cron schedule expression")
	u.String("image", "", "New Docker image")
	u.StringArray("command", nil, "New container command (replaces existing; repeat flag for each argument)")
	u.StringArray("volume", nil, "New bind mounts (replaces existing; repeat flag for each mount)")
	u.String("cpu-limit", "", "New CPU limit")
	u.String("memory-limit", "", "New memory limit")
	u.Int("timeout", 0, "New execution timeout in seconds")

	jobCmd.AddCommand(jobAddCmd)
	jobCmd.AddCommand(jobUpdateCmd)
	jobCmd.AddCommand(jobRemoveCmd)
	jobCmd.AddCommand(jobListCmd)
	jobCmd.AddCommand(jobEnableCmd)
	jobCmd.AddCommand(jobDisableCmd)
}

func runJobAdd(cmd *cobra.Command, args []string) error {
	if err := validateSchedule(jobAddFlags.schedule); err != nil {
		return err
	}

	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	now := time.Now()
	job := store.Job{
		ID:             uuid.New().String(),
		Name:           jobAddFlags.name,
		Schedule:       jobAddFlags.schedule,
		Image:          jobAddFlags.image,
		Command:        jobAddFlags.command,
		Volumes:        jobAddFlags.volumes,
		CPULimit:       jobAddFlags.cpuLimit,
		MemoryLimit:    jobAddFlags.memoryLimit,
		TimeoutSeconds: jobAddFlags.timeoutSeconds,
		Enabled:        true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := st.CreateJob(context.Background(), job); err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	fmt.Printf("Job %q added (id: %s)\n", job.Name, job.ID)
	return nil
}

func runJobUpdate(cmd *cobra.Command, args []string) error {
	name := args[0]

	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	ctx := context.Background()
	job, err := st.GetJob(ctx, name)
	if err != nil {
		return err
	}

	changed := false
	f := cmd.Flags()

	if f.Changed("schedule") {
		sched, _ := f.GetString("schedule")
		if err := validateSchedule(sched); err != nil {
			return err
		}
		job.Schedule = sched
		changed = true
	}
	if f.Changed("image") {
		job.Image, _ = f.GetString("image")
		changed = true
	}
	if f.Changed("command") {
		job.Command, _ = f.GetStringArray("command")
		changed = true
	}
	if f.Changed("volume") {
		job.Volumes, _ = f.GetStringArray("volume")
		changed = true
	}
	if f.Changed("cpu-limit") {
		job.CPULimit, _ = f.GetString("cpu-limit")
		changed = true
	}
	if f.Changed("memory-limit") {
		job.MemoryLimit, _ = f.GetString("memory-limit")
		changed = true
	}
	if f.Changed("timeout") {
		job.TimeoutSeconds, _ = f.GetInt("timeout")
		changed = true
	}

	if !changed {
		fmt.Println("Nothing to update — specify at least one flag.")
		return nil
	}

	job.UpdatedAt = time.Now()
	if err := st.UpdateJob(ctx, *job); err != nil {
		return fmt.Errorf("update job: %w", err)
	}
	fmt.Printf("Job %q updated (scheduler will pick up changes within 15s)\n", name)
	return nil
}

// ── job remove ────────────────────────────────────────────────────────────────

var jobRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a job by name",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		st, err := openStore()
		if err != nil {
			return err
		}
		defer st.Close()

		if err := st.DeleteJob(context.Background(), name); err != nil {
			return err
		}
		fmt.Printf("Job %q removed\n", name)
		return nil
	},
}

// ── job list ──────────────────────────────────────────────────────────────────

var jobListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all registered jobs",
	RunE: func(cmd *cobra.Command, args []string) error {
		st, err := openStore()
		if err != nil {
			return err
		}
		defer st.Close()

		jobs, err := st.ListJobs(context.Background())
		if err != nil {
			return fmt.Errorf("list jobs: %w", err)
		}
		if len(jobs) == 0 {
			fmt.Println("No jobs registered. Use 'saturn job add' to add one.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tSCHEDULE\tIMAGE\tENABLED\tTIMEOUT\tCREATED")
		for _, j := range jobs {
			enabled := "yes"
			if !j.Enabled {
				enabled = "no"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%ds\t%s\n",
				j.Name,
				j.Schedule,
				j.Image,
				enabled,
				j.TimeoutSeconds,
				j.CreatedAt.Format("2006-01-02 15:04"),
			)
		}
		return w.Flush()
	},
}

// ── job enable/disable ────────────────────────────────────────────────────────

var jobEnableCmd = &cobra.Command{
	Use:   "enable <name>",
	Short: "Enable a disabled job",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return setEnabled(args[0], true) },
}

var jobDisableCmd = &cobra.Command{
	Use:   "disable <name>",
	Short: "Disable a job without removing it",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return setEnabled(args[0], false) },
}

func setEnabled(name string, enabled bool) error {
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	ctx := context.Background()
	job, err := st.GetJob(ctx, name)
	if err != nil {
		return err
	}
	job.Enabled = enabled
	job.UpdatedAt = time.Now()
	if err := st.UpdateJob(ctx, *job); err != nil {
		return err
	}
	action := "enabled"
	if !enabled {
		action = "disabled"
	}
	fmt.Printf("Job %q %s\n", name, action)
	return nil
}

// validateSchedule returns an error if expr is not a valid 5-field cron expression.
func validateSchedule(expr string) error {
	p := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := p.Parse(expr); err != nil {
		return fmt.Errorf("invalid cron schedule %q: %w", expr, err)
	}
	return nil
}

// openStore opens the SQLite store from the saturn state directory.
func openStore() (*store.SQLiteStore, error) {
	dir, err := ensureSaturnDir()
	if err != nil {
		return nil, err
	}
	st, err := store.NewSQLiteStore(filepath.Join(dir, "saturn.db"))
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	return st, nil
}
