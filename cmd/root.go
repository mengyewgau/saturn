package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "saturn",
	Short: "A CLI cronjob manager backed by Docker",
	Long:  "saturn registers, schedules, and executes Docker-based cronjobs with a persistent SQLite store.",
}

// Execute is the main entry point called from main.go.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(jobCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(statusCmd)
}

// saturnDir returns the path to the saturn state directory (~/.saturn).
func saturnDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".saturn"
	}
	return filepath.Join(home, ".saturn")
}

// ensureSaturnDir creates the saturn state directory if it does not exist.
func ensureSaturnDir() (string, error) {
	dir := saturnDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create saturn dir %s: %w", dir, err)
	}
	return dir, nil
}
