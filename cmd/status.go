package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon and job health summary",
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	dir, err := ensureSaturnDir()
	if err != nil {
		return err
	}

	// ── Daemon status ────────────────────────────────────────────────────────
	pidPath := filepath.Join(dir, "saturn.pid")
	if data, err := os.ReadFile(pidPath); err == nil {
		pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		proc, err := os.FindProcess(pid)
		if err == nil && proc.Signal(syscall.Signal(0)) == nil {
			fmt.Printf("Daemon:   running (PID %d)\n", pid)
		} else {
			fmt.Printf("Daemon:   not running (stale PID %d)\n", pid)
		}
	} else {
		fmt.Println("Daemon:   not running")
	}

	dbPath := filepath.Join(dir, "saturn.db")
	logPath := filepath.Join(dir, "saturn.log")
	fmt.Printf("Database: %s\n", dbPath)
	fmt.Printf("Log file: %s\n\n", logPath)

	// ── Job summary ──────────────────────────────────────────────────────────
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	ctx := context.Background()
	jobs, err := st.ListJobs(ctx)
	if err != nil {
		return fmt.Errorf("list jobs: %w", err)
	}

	enabled := 0
	for _, j := range jobs {
		if j.Enabled {
			enabled++
		}
	}
	fmt.Printf("Jobs: %d total, %d enabled\n\n", len(jobs), enabled)

	if len(jobs) == 0 {
		fmt.Println("No jobs registered. Use 'saturn job add' to add one.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATE\tLAST RUN\tLAST STATUS")
	for _, j := range jobs {
		state := "enabled"
		if !j.Enabled {
			state = "disabled"
		}
		lastRun := "never"
		lastStatus := "-"
		runs, err := st.ListRuns(ctx, j.ID, 1)
		if err == nil && len(runs) > 0 {
			lastRun = runs[0].StartedAt.Format("2006-01-02 15:04:05")
			lastStatus = runs[0].Status
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", j.Name, state, lastRun, lastStatus)
	}
	return w.Flush()
}
