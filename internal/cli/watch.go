package cli

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/dandriscoll/filehook/internal/config"
	"github.com/dandriscoll/filehook/internal/plugin"
	"github.com/dandriscoll/filehook/internal/queue"
	"github.com/dandriscoll/filehook/internal/watcher"
	"github.com/dandriscoll/filehook/internal/worker"
	"github.com/spf13/cobra"
)

var watchPattern string

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Start watcher and workers",
	Long:  "Watch for input files and process them as they appear or change.",
	RunE:  runWatch,
}

func init() {
	watchCmd.Flags().StringVarP(&watchPattern, "pattern", "p", "", "only process files matching the named pattern")
	rootCmd.AddCommand(watchCmd)
}

func runWatch(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if err := validateConfig(cfg); err != nil {
		return err
	}

	// In dry-run mode, show what would be watched
	if isDryRun() {
		return watchDryRun(cfg)
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

	// Cleanup stale running jobs from previous run
	cleaned, err := store.CleanupStaleRunning(ctx)
	if err != nil {
		return fmt.Errorf("failed to cleanup stale jobs: %w", err)
	}
	if cleaned > 0 {
		logger.Printf("Reset %d stale running jobs to pending", cleaned)
	}

	// Initialize plugins
	namingPlugin, err := plugin.NewNamingPlugin(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize naming plugin: %w", err)
	}

	shouldProcess := plugin.NewShouldProcessChecker(cfg)
	groupKeyGen := plugin.NewGroupKeyGenerator(cfg)

	// Initialize watcher
	matcher := watcher.NewMatcherWithFilter(cfg.Inputs.Patterns, cfg.Watch.Ignore, watchPattern)
	w, err := watcher.New(matcher, cfg.Watch.DebounceDuration())
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	defer w.Close()

	// Add watch paths (use -d directory if specified, otherwise config's watch paths)
	for _, path := range getEffectiveWatchPaths(cfg) {
		if watchPattern != "" {
			logger.Printf("Watching: %s (pattern filter: %s)", path, watchPattern)
		} else {
			logger.Printf("Watching: %s", path)
		}
		if err := w.AddPath(path); err != nil {
			return fmt.Errorf("failed to add watch path: %w", err)
		}
	}

	// Start watcher
	w.Start(ctx)

	// Start workers
	var stopWorkers func()
	if cfg.Concurrency.Mode == config.ConcurrencySequentialSwitch {
		scheduler, err := worker.NewSequentialScheduler(cfg, store, logger)
		if err != nil {
			return fmt.Errorf("failed to create scheduler: %w", err)
		}
		scheduler.Start(ctx)
		stopWorkers = scheduler.Stop
	} else {
		pool, err := worker.NewPool(cfg, store, logger)
		if err != nil {
			return fmt.Errorf("failed to create worker pool: %w", err)
		}
		pool.Start(ctx)
		stopWorkers = pool.Stop
	}
	defer stopWorkers()

	logger.Printf("Started with %d workers in %s mode", cfg.Concurrency.MaxWorkers, cfg.Concurrency.Mode)

	// Process events
	for {
		select {
		case <-ctx.Done():
			return nil

		case event := <-w.Events():
			if err := processEvent(ctx, cfg, store, namingPlugin, shouldProcess, groupKeyGen, event, logger); err != nil {
				logger.Printf("Error processing %s: %v", event.Path, err)
			}

		case err := <-w.Errors():
			logger.Printf("Watcher error: %v", err)
		}
	}
}

func processEvent(
	ctx context.Context,
	cfg *config.Config,
	store queue.Store,
	namingPlugin *plugin.NamingPlugin,
	shouldProcess *plugin.ShouldProcessChecker,
	groupKeyGen *plugin.GroupKeyGenerator,
	event watcher.Event,
	logger *log.Logger,
) error {
	// Check if already pending/running
	exists, err := store.HasPendingOrRunning(ctx, event.Path)
	if err != nil {
		return fmt.Errorf("failed to check existing job: %w", err)
	}
	if exists {
		logger.Printf("Skipping %s: already queued", event.Path)
		return nil
	}

	// Generate output filenames and check readiness
	// The naming plugin returns both outputs and ready status in one call
	naming, err := namingPlugin.Generate(ctx, event.Path)
	if err != nil {
		return fmt.Errorf("naming plugin failed: %w", err)
	}

	// Check if file is ready for transformation (from naming plugin's "ready" field)
	if !naming.Ready {
		logger.Printf("Skipping %s: not ready for transformation", event.Path)
		return nil
	}

	// Check if we should process (based on modification policy, NOT readiness)
	shouldProc, reason, err := shouldProcess.Check(ctx, event.Path, naming.Outputs, event.IsModify)
	if err != nil {
		return fmt.Errorf("should_process check failed: %w", err)
	}

	if !shouldProc {
		logger.Printf("Skipping %s: %s", event.Path, reason)
		return nil
	}

	// Get group key
	var groupKey string
	if groupKeyGen != nil {
		groupKey, err = groupKeyGen.Generate(ctx, event.Path)
		if err != nil {
			logger.Printf("Warning: group key generation failed for %s: %v", event.Path, err)
			// Fall through to default
		}
	}
	if groupKey == "" {
		// Default grouping
		if len(cfg.WatchPaths()) > 0 {
			groupKey = plugin.DefaultGroupKey(event.Path, cfg.WatchPaths()[0])
		}
	}

	// Handle versioned policy
	outputs := naming.Outputs
	if cfg.OnModified == config.ModifiedVersioned && event.IsModify {
		// Find next version number
		version := 1
		for {
			versioned := worker.VersionedOutputPaths(outputs, version)
			allExist := true
			for _, p := range versioned {
				if _, err := os.Stat(p); os.IsNotExist(err) {
					allExist = false
					break
				}
			}
			if !allExist {
				outputs = versioned
				break
			}
			version++
			if version > 1000 {
				return fmt.Errorf("too many versions")
			}
		}
	}

	// Resolve command: use pattern-specific or fall back to global
	var command []string
	if event.Pattern != nil && event.Pattern.HasCommand() {
		command = event.Pattern.Command.AsSlice()
	} else {
		command = cfg.Command.AsSlice()
	}

	// Enqueue job
	job := &queue.Job{
		InputPath:   event.Path,
		OutputPaths: outputs,
		GroupKey:    groupKey,
		Command:     command,
	}

	if err := store.Enqueue(ctx, job); err != nil {
		return fmt.Errorf("failed to enqueue: %w", err)
	}

	logger.Printf("Queued: %s -> %v (reason: %s)", event.Path, outputs, reason)
	return nil
}

// watchDryRun prints what the watch command would do
func watchDryRun(cfg *config.Config) error {
	fmt.Println("Dry-run mode: showing watch configuration")
	fmt.Println()

	fmt.Printf("Watch paths:\n")
	for _, path := range cfg.WatchPaths() {
		fmt.Printf("  - %s\n", path)
	}
	fmt.Println()

	fmt.Printf("Input patterns: %v\n", cfg.Inputs.Patterns)
	if watchPattern != "" {
		fmt.Printf("Pattern filter: %s\n", watchPattern)
	}
	if len(cfg.Watch.Ignore) > 0 {
		fmt.Printf("Ignore patterns: %v\n", cfg.Watch.Ignore)
	}
	fmt.Println()

	fmt.Printf("Command: %v\n", cfg.Command.AsSlice())
	fmt.Println()

	fmt.Printf("Concurrency: %s mode with %d workers\n", cfg.Concurrency.Mode, cfg.Concurrency.MaxWorkers)
	fmt.Printf("Debounce: %s\n", cfg.Watch.DebounceDuration())
	fmt.Println()

	fmt.Println("When a matching file is created or modified:")
	fmt.Println("  1. Check if file matches input patterns")
	fmt.Println("  2. Run naming plugin to get outputs and ready status")
	fmt.Println("  3. Skip if ready=false (file not ready for transformation)")
	fmt.Println("  4. Run should_process check (policy-based: timestamps, output existence)")
	fmt.Println("  5. Queue job for processing")
	fmt.Println("  6. Execute command with input/output substitution")

	return nil
}
