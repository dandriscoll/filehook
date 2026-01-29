package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dandriscoll/filehook/internal/debug"
	"github.com/dandriscoll/filehook/internal/output"
	"github.com/dandriscoll/filehook/internal/plugin"
	"github.com/dandriscoll/filehook/internal/queue"
	"github.com/dandriscoll/filehook/internal/worker"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the central scheduler (dequeue and execute jobs)",
	Long: `Start a central scheduler that polls the shared queue, manages stack
switching, and executes jobs. Only one scheduler can be active at a time.

When a scheduler is running, watch/run commands automatically become
producer-only (enqueue without executing).`,
	RunE: runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if err := validateConfig(cfg); err != nil {
		return err
	}

	// Setup logger
	logger := output.NewLogger()

	// Setup debug logger
	debugLogger, err := debug.New(cfg.StateDirectory(), cfg.Debug)
	if err != nil {
		return fmt.Errorf("failed to initialize debug logger: %w", err)
	}
	defer debugLogger.Close()

	if cfg.Debug {
		logger.Info("debug logging enabled: %s/debug.log", cfg.StateDirectory())
	}

	// Setup context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("shutting down…")
		cancel()
	}()

	// Initialize store
	store, err := queue.NewSQLiteStore(cfg.StateDirectory())
	if err != nil {
		return fmt.Errorf("failed to open queue: %w", err)
	}
	defer store.Close()

	if err := store.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize queue: %w", err)
	}

	// Check for existing live scheduler
	existingScheduler, err := store.GetSchedulerProcess(ctx)
	if err != nil {
		return fmt.Errorf("failed to check for existing scheduler: %w", err)
	}
	if existingScheduler != nil {
		return fmt.Errorf("another scheduler is already running (PID %d, started %s)",
			existingScheduler.PID, existingScheduler.StartedAt.Format(time.RFC3339))
	}

	// Clean up stale entries from dead schedulers and their claimed jobs
	allProcesses, err := store.GetActiveProcesses(ctx)
	if err != nil {
		logger.Warn("failed to get active processes: %v", err)
	}
	// GetActiveProcesses already cleaned dead PIDs from process_info.
	// Now clean up jobs claimed by PIDs that are no longer in the active list.
	// We check all running jobs with claimed_by set.
	cleaned, err := store.CleanupStaleRunning(ctx)
	if err != nil {
		logger.Warn("failed to cleanup stale jobs: %v", err)
	}
	if cleaned > 0 {
		logger.Info("reset %d stale jobs to pending", cleaned)
	}
	_ = allProcesses

	// Register this process as scheduler
	pid := os.Getpid()
	processInfo := &queue.ProcessInfo{
		PID:       pid,
		Command:   "serve",
		Role:      queue.ProcessRoleScheduler,
		StartedAt: time.Now(),
	}
	if err := store.RegisterProcess(ctx, processInfo); err != nil {
		return fmt.Errorf("failed to register scheduler process: %w", err)
	}
	defer func() {
		if err := store.UnregisterProcess(context.Background(), pid); err != nil {
			logger.Warn("failed to unregister process: %v", err)
		}
	}()

	// Initialize plugins
	namingPlugin, err := plugin.NewNamingPlugin(cfg, debugLogger)
	if err != nil {
		return fmt.Errorf("failed to initialize naming plugin: %w", err)
	}

	// Create and start the central scheduler
	scheduler, err := worker.NewCentralScheduler(cfg, store, namingPlugin, debugLogger, logger, pid)
	if err != nil {
		return fmt.Errorf("failed to create central scheduler: %w", err)
	}

	scheduler.Start(ctx)
	logger.Banner("filehook serve (scheduler)")
	logger.Info("PID %d", pid)

	// Wait for context cancellation
	<-ctx.Done()
	scheduler.Stop()

	logger.Info("scheduler stopped")
	return nil
}
