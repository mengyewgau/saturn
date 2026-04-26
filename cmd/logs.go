package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

var logsLast int

var logsCmd = &cobra.Command{
	Use:   "logs <name>",
	Short: "Show execution logs for a job",
	Args:  cobra.ExactArgs(1),
	RunE:  runLogs,
}

func init() {
	logsCmd.Flags().IntVar(&logsLast, "last", 10, "Number of most recent runs to display")
}

func runLogs(cmd *cobra.Command, args []string) error {
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

	runs, err := st.ListRuns(ctx, job.ID, logsLast)
	if err != nil {
		return fmt.Errorf("list runs: %w", err)
	}
	if len(runs) == 0 {
		fmt.Printf("No runs found for job %q\n", name)
		return nil
	}

	for i, run := range runs {
		duration := ""
		if run.FinishedAt != nil {
			duration = fmt.Sprintf(", duration: %s", run.FinishedAt.Sub(run.StartedAt).Round(0))
		}
		errStr := ""
		if run.ErrorMessage != nil {
			errStr = fmt.Sprintf(", error: %s", *run.ErrorMessage)
		}
		fmt.Printf("=== Run %d | %s | status: %s | exit: %d%s%s ===\n",
			i+1,
			run.StartedAt.Format("2006-01-02 15:04:05"),
			run.Status,
			run.ExitCode,
			duration,
			errStr,
		)
		if run.LogOutput != "" {
			fmt.Println(run.LogOutput)
		} else {
			fmt.Println("(no output)")
		}
	}
	return nil
}
