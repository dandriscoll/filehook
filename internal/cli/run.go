package cli

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/dandriscoll/filehook/internal/config"
	"github.com/dandriscoll/filehook/internal/plugin"
	"github.com/dandriscoll/filehook/internal/queue"
	"github.com/dandriscoll/filehook/internal/watcher"
	"github.com/dandriscoll/filehook/internal/worker"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "One-shot scan and process until queue empty",
	Long:  "Scan for input files, queue them, and process until the queue is empty.",
	RunE:  runRun,
}

func init() {
	rootCmd.AddCommand(runCmd)
}

func runRun(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if err := validateConfig(cfg); err != nil {
		return err
	}

	// In dry-run mode, use a simplified flow
	if isDryRun() {
		return runDryRun(cfg)
	}

	// Setup logger
	logger := log.New(os.Stdout, "[filehook] ", log.LstdFlags)

	// Setup context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Println("Shutting down...")
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

	// Cleanup stale running jobs
	cleaned, err := store.CleanupStaleRunning(ctx)
	if err != nil {
		return fmt.Errorf("failed to cleanup stale jobs: %w", err)
	}
	if cleaned > 0 {
		logger.Printf("Reset %d stale running jobs to pending", cleaned)
	}

	// Initialize plugins
	filenameGen, err := plugin.NewFilenameGenerator(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize filename generator: %w", err)
	}

	shouldProcess := plugin.NewShouldProcessChecker(cfg)
	groupKeyGen := plugin.NewGroupKeyGenerator(cfg)

	// Initialize ready checker (optional - nil if not supported)
	readyChecker, err := plugin.NewReadyChecker(cfg)
	if err != nil {
		logger.Printf("Ready checker not available: %v", err)
		readyChecker = nil
	}

	// Create matcher for scanning
	matcher := watcher.NewMatcher(cfg.Inputs.Patterns, cfg.Watch.Ignore)

	// Create a temporary watcher just for scanning
	w, err := watcher.New(matcher, cfg.Watch.DebounceDuration())
	if err != nil {
		return fmt.Errorf("failed to create scanner: %w", err)
	}
	defer w.Close()

	// Scan existing files
	logger.Println("Scanning for input files...")

	// Start consuming events in a goroutine
	eventsDone := make(chan struct{})
	go func() {
		defer close(eventsDone)
		for event := range w.Events() {
			if err := processEvent(ctx, cfg, store, filenameGen, shouldProcess, groupKeyGen, readyChecker, event, logger); err != nil {
				logger.Printf("Error processing %s: %v", event.Path, err)
			}
		}
	}()

	// Scan files (this sends events to the channel)
	if err := w.ScanExisting(ctx, cfg.WatchPaths()); err != nil {
		return fmt.Errorf("failed to scan: %w", err)
	}

	// Close the watcher to signal event goroutine to stop
	w.Close()
	<-eventsDone

	// Get queue stats
	stats, err := store.GetStats(ctx)
	if err != nil {
		return fmt.Errorf("failed to get stats: %w", err)
	}

	logger.Printf("Found %d pending jobs", stats.Pending)

	if stats.Pending == 0 {
		logger.Println("No jobs to process")
		return nil
	}

	// Process all jobs
	logger.Println("Processing jobs...")

	if cfg.Concurrency.Mode == config.ConcurrencySequentialSwitch {
		scheduler, err := worker.NewSequentialScheduler(cfg, store, logger)
		if err != nil {
			return fmt.Errorf("failed to create scheduler: %w", err)
		}
		if err := scheduler.RunOnce(ctx); err != nil {
			return err
		}
	} else {
		pool, err := worker.NewPool(cfg, store, logger)
		if err != nil {
			return fmt.Errorf("failed to create worker pool: %w", err)
		}
		pool.Start(ctx)
		// Wait for queue to empty
		for {
			select {
			case <-ctx.Done():
				pool.Stop()
				return ctx.Err()
			default:
			}

			stats, err := store.GetStats(ctx)
			if err != nil {
				pool.Stop()
				return err
			}

			if stats.Pending == 0 && stats.Running == 0 {
				pool.Stop()
				break
			}

			// Small sleep to avoid busy loop
			select {
			case <-ctx.Done():
				pool.Stop()
				return ctx.Err()
			default:
			}
		}
	}

	// Final stats
	stats, err = store.GetStats(ctx)
	if err != nil {
		return fmt.Errorf("failed to get final stats: %w", err)
	}

	logger.Printf("Completed: %d successful, %d failed", stats.Completed, stats.Failed)

	if stats.Failed > 0 {
		return fmt.Errorf("%d jobs failed", stats.Failed)
	}

	return nil
}

// runDryRun performs a dry-run scan and prints what would be done
func runDryRun(cfg *config.Config) error {
	fmt.Println("Dry-run mode: showing what would be done")
	fmt.Println()

	ctx := context.Background()

	// Initialize plugins
	filenameGen, err := plugin.NewFilenameGenerator(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize filename generator: %w", err)
	}

	shouldProcess := plugin.NewShouldProcessChecker(cfg)

	// Create matcher for scanning
	matcher := watcher.NewMatcher(cfg.Inputs.Patterns, cfg.Watch.Ignore)

	// Create a temporary watcher just for scanning
	w, err := watcher.New(matcher, cfg.Watch.DebounceDuration())
	if err != nil {
		return fmt.Errorf("failed to create scanner: %w", err)
	}
	defer w.Close()

	fmt.Printf("Watch paths: %v\n", cfg.WatchPaths())
	fmt.Printf("Input patterns: %v\n", cfg.Inputs.Patterns)
	if len(cfg.Watch.Ignore) > 0 {
		fmt.Printf("Ignore patterns: %v\n", cfg.Watch.Ignore)
	}
	fmt.Printf("Command: %v\n", cfg.Command.AsSlice())
	fmt.Println()

	// Collect events
	var jobs []dryRunJob
	eventsDone := make(chan struct{})
	go func() {
		defer close(eventsDone)
		for event := range w.Events() {
			job, skip, reason := buildDryRunJob(ctx, cfg, filenameGen, shouldProcess, event)
			if skip {
				fmt.Printf("Skip: %s (%s)\n", event.Path, reason)
				continue
			}
			jobs = append(jobs, job)
		}
	}()

	// Scan files
	if err := w.ScanExisting(ctx, cfg.WatchPaths()); err != nil {
		return fmt.Errorf("failed to scan: %w", err)
	}

	w.Close()
	<-eventsDone

	fmt.Println()
	if len(jobs) == 0 {
		fmt.Println("No files to process")
		return nil
	}

	fmt.Printf("Would process %d file(s):\n", len(jobs))
	fmt.Println()
	for i, job := range jobs {
		fmt.Printf("%d. %s\n", i+1, job.InputPath)
		fmt.Printf("   Output: %v\n", job.OutputPaths)
		fmt.Printf("   Command: %v\n", job.Command)
		fmt.Println()
	}

	return nil
}

// dryRunJob represents a job that would be executed
type dryRunJob struct {
	InputPath   string
	OutputPaths []string
	Command     []string
}

// buildDryRunJob creates a dry-run job from an event
func buildDryRunJob(
	ctx context.Context,
	cfg *config.Config,
	filenameGen *plugin.FilenameGenerator,
	shouldProcess *plugin.ShouldProcessChecker,
	event watcher.Event,
) (dryRunJob, bool, string) {
	// Generate output filenames
	outputs, err := filenameGen.Generate(ctx, event.Path)
	if err != nil {
		return dryRunJob{}, true, fmt.Sprintf("filename gen failed: %v", err)
	}

	// Check if we should process
	shouldProc, reason, err := shouldProcess.Check(ctx, event.Path, outputs, event.IsModify)
	if err != nil {
		return dryRunJob{}, true, fmt.Sprintf("should_process failed: %v", err)
	}
	if !shouldProc {
		return dryRunJob{}, true, reason
	}

	// Build command preview
	cmdSlice := cfg.Command.AsSlice()
	command := make([]string, len(cmdSlice))
	for i, arg := range cmdSlice {
		arg = strings.ReplaceAll(arg, "{{input}}", event.Path)
		if len(outputs) > 0 {
			arg = strings.ReplaceAll(arg, "{{output}}", outputs[0])
		}
		arg = strings.ReplaceAll(arg, "{{outputs}}", strings.Join(outputs, " "))
		command[i] = arg
	}

	return dryRunJob{
		InputPath:   event.Path,
		OutputPaths: outputs,
		Command:     command,
	}, false, reason
}
