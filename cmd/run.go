package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/yourusername/saturn/internal/executor"
	"github.com/yourusername/saturn/internal/logger"
	"github.com/yourusername/saturn/internal/scheduler"
	"github.com/yourusername/saturn/internal/store"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Start the saturn scheduler daemon",
	RunE:  runDaemon,
}

func runDaemon(cmd *cobra.Command, args []string) error {
	dir, err := ensureSaturnDir()
	if err != nil {
		return err
	}

	logPath := filepath.Join(dir, "saturn.log")
	log := logger.New(logPath)

	// Write PID file so `saturn status` can detect a running daemon.
	pidPath := filepath.Join(dir, "saturn.pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer os.Remove(pidPath)

	dbPath := filepath.Join(dir, "saturn.db")
	st, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	exec, err := executor.NewDockerExecutor()
	if err != nil {
		return fmt.Errorf("create Docker client: %w", err)
	}

	// Fail fast if Docker daemon is unreachable.
	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := exec.Ping(pingCtx); err != nil {
		return fmt.Errorf("Docker daemon unreachable: %w", err)
	}

	sched := scheduler.New(st, exec, log)
	if err := sched.Start(context.Background()); err != nil {
		return fmt.Errorf("start scheduler: %w", err)
	}

	log.Info("saturn started", "pid", os.Getpid(), "db", dbPath, "log", logPath)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	log.Info("saturn shutting down")
	sched.Stop()
	return nil
}
