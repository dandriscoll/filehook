package cli

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/dandriscoll/filehook/internal/config"
	"github.com/dandriscoll/filehook/internal/debug"
	"github.com/dandriscoll/filehook/internal/plugin"
	"github.com/dandriscoll/filehook/internal/queue"
	"github.com/dandriscoll/filehook/internal/watcher"
	"github.com/dandriscoll/filehook/internal/worker"
	"github.com/spf13/cobra"
)

var (
	watchPattern       string
	watchInstanceID    string
	watchDefaultPriority int
)

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Start watcher and workers",
	Long:  "Watch for input files and process them as they appear or change.",
	RunE:  runWatch,
}

func init() {
	watchCmd.Flags().StringVarP(&watchPattern, "pattern", "p", "", "only process files matching the named pattern")
	watchCmd.Flags().StringVar(&watchInstanceID, "instance", "", "instance name for job tagging (default: config file basename)")
	watchCmd.Flags().IntVar(&watchDefaultPriority, "priority", 0, "default priority for all jobs from this instance")
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

	// Validate pattern filter if specified
	if err := validatePatternFilter(cfg, watchPattern); err != nil {
		return err
	}

	// In dry-run mode, show what would be watched
	if isDryRun() {
		return watchDryRun(cfg)
	}

	// Setup logger
	logger := log.New(os.Stdout, "[filehook] ", log.LstdFlags)

	// Setup debug logger
	debugLogger, err := debug.New(cfg.StateDirectory(), cfg.Debug)
	if err != nil {
		return fmt.Errorf("failed to initialize debug logger: %w", err)
	}
	defer debugLogger.Close()

	if cfg.Debug {
		logger.Printf("Debug logging enabled: %s/debug.log", cfg.StateDirectory())
	}

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

	// Resolve instance ID
	instanceID := resolveInstanceID(watchInstanceID, cfgFile)

	// Check for a live scheduler to determine mode
	schedulerProc, _ := store.GetSchedulerProcess(ctx)
	producerMode := schedulerProc != nil

	if producerMode {
		logger.Printf("Scheduler detected (PID %d), running in producer mode", schedulerProc.PID)
	} else {
		logger.Println("No scheduler detected, running in legacy mode")
		// Cleanup stale running jobs from previous run (only in legacy mode)
		cleaned, err := store.CleanupStaleRunning(ctx)
		if err != nil {
			return fmt.Errorf("failed to cleanup stale jobs: %w", err)
		}
		if cleaned > 0 {
			logger.Printf("Reset %d stale running jobs to pending", cleaned)
		}
	}

	// Register this process
	pid := os.Getpid()
	role := queue.ProcessRoleLegacy
	if producerMode {
		role = queue.ProcessRoleProducer
	}
	processInfo := &queue.ProcessInfo{
		PID:        pid,
		Command:    "watch",
		Role:       role,
		InstanceID: instanceID,
		StartedAt:  time.Now(),
	}
	if err := store.RegisterProcess(ctx, processInfo); err != nil {
		logger.Printf("Warning: failed to register process: %v", err)
	}
	defer func() {
		// Unregister on shutdown
		if err := store.UnregisterProcess(context.Background(), pid); err != nil {
			logger.Printf("Warning: failed to unregister process: %v", err)
		}
	}()

	// Initialize plugins
	namingPlugin, err := plugin.NewNamingPlugin(cfg, debugLogger)
	if err != nil {
		return fmt.Errorf("failed to initialize naming plugin: %w", err)
	}

	shouldProcess := plugin.NewShouldProcessChecker(cfg, debugLogger)
	groupKeyGen := plugin.NewGroupKeyGenerator(cfg, debugLogger)

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

	// Start workers only in legacy mode (no scheduler present)
	var stopWorkers func()
	if !producerMode {
		switch cfg.Concurrency.Mode {
		case config.ConcurrencySequentialSwitch:
			scheduler, err := worker.NewSequentialScheduler(cfg, store, namingPlugin, debugLogger, logger)
			if err != nil {
				return fmt.Errorf("failed to create scheduler: %w", err)
			}
			scheduler.Start(ctx)
			stopWorkers = scheduler.Stop
		case config.ConcurrencyStack:
			scheduler, err := worker.NewStackScheduler(cfg, store, namingPlugin, debugLogger, logger)
			if err != nil {
				return fmt.Errorf("failed to create stack scheduler: %w", err)
			}
			scheduler.Start(ctx)
			stopWorkers = scheduler.Stop
		default:
			pool, err := worker.NewPool(cfg, store, namingPlugin, debugLogger, logger)
			if err != nil {
				return fmt.Errorf("failed to create worker pool: %w", err)
			}
			pool.Start(ctx)
			stopWorkers = pool.Stop
		}

		if cfg.Concurrency.Mode == config.ConcurrencyStack {
			logger.Printf("Started in %s mode with %d stacks defined", cfg.Concurrency.Mode, len(cfg.Stacks.Definitions))
		} else {
			logger.Printf("Started with %d workers in %s mode", cfg.Concurrency.MaxWorkers, cfg.Concurrency.Mode)
		}
	}
	defer func() {
		if stopWorkers != nil {
			stopWorkers()
		}
	}()

	evtOpts := eventOptions{instanceID: instanceID, defaultPriority: watchDefaultPriority}

	// Initial scan of existing files (like run mode)
	logger.Println("Scanning for existing files...")

	// Process scan events in a goroutine while scanning
	scanDone := make(chan struct{})
	scannerStopped := make(chan struct{})
	go func() {
		defer close(scannerStopped)
		for {
			select {
			case event, ok := <-w.Events():
				if !ok {
					return
				}
				if err := processEvent(ctx, cfg, store, namingPlugin, shouldProcess, groupKeyGen, event, logger, debugLogger, evtOpts); err != nil {
					logger.Printf("Error processing %s: %v", event.Path, err)
				}
			case <-scanDone:
				// Drain any remaining events from the scan
				for {
					select {
					case event, ok := <-w.Events():
						if !ok {
							return
						}
						if err := processEvent(ctx, cfg, store, namingPlugin, shouldProcess, groupKeyGen, event, logger, debugLogger, evtOpts); err != nil {
							logger.Printf("Error processing %s: %v", event.Path, err)
						}
					default:
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	if err := w.ScanExisting(ctx, getEffectiveWatchPaths(cfg)); err != nil {
		logger.Printf("Warning: scan failed: %v", err)
	}
	close(scanDone)
	<-scannerStopped

	// Get stats after scan
	stats, err := store.GetStats(ctx)
	if err == nil && stats.Pending > 0 {
		logger.Printf("Found %d files to process", stats.Pending)
	}

	logger.Println("Watching for changes...")

	// Continue watching for new events
	for {
		select {
		case <-ctx.Done():
			return nil

		case event := <-w.Events():
			if err := processEvent(ctx, cfg, store, namingPlugin, shouldProcess, groupKeyGen, event, logger, debugLogger, evtOpts); err != nil {
				logger.Printf("Error processing %s: %v", event.Path, err)
			}

		case err := <-w.Errors():
			logger.Printf("Watcher error: %v", err)
		}
	}
}

// eventOptions holds optional parameters for processEvent
type eventOptions struct {
	instanceID      string
	defaultPriority int
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
	debugLogger *debug.Logger,
	opts ...eventOptions,
) error {
	eventType := "CREATE"
	if event.IsModify {
		eventType = "MODIFY"
	}
	debugLogger.Event(eventType, event.Path, fmt.Sprintf("pattern=%v", event.Pattern != nil))

	// Check if already pending/running
	exists, err := store.HasPendingOrRunning(ctx, event.Path)
	if err != nil {
		return fmt.Errorf("failed to check existing job: %w", err)
	}
	if exists {
		debugLogger.Decision(event.Path, "SKIP", "already queued")
		logger.Printf("Skipping %s: already queued", event.Path)
		return nil
	}

	// Get target type from matched pattern
	targetType := ""
	if event.Pattern != nil {
		targetType = event.Pattern.TargetType
	}
	debugLogger.Log("Processing %s: targetType=%s, isModify=%v", event.Path, targetType, event.IsModify)

	// Generate output filenames and check readiness
	// The naming plugin calls "ready" and "propose" operations separately
	// Note: outputs are calculated here for shouldProcess check, but will be
	// recalculated at execution time to get fresh values
	naming, err := namingPlugin.Generate(ctx, event.Path, targetType)
	if err != nil {
		debugLogger.Decision(event.Path, "ERROR", fmt.Sprintf("naming plugin failed: %v", err))
		return fmt.Errorf("naming plugin failed: %w", err)
	}
	debugLogger.Log("Naming result: ready=%v, outputs=%v", naming.Ready, naming.Outputs)

	// Check if file is ready for transformation (from naming plugin's "ready" field)
	if !naming.Ready {
		debugLogger.Decision(event.Path, "SKIP", "not ready for transformation")
		logger.Printf("Skipping %s: not ready for transformation", event.Path)
		return nil
	}

	// Check if we should process (based on modification policy, NOT readiness)
	// Use the outputs from naming plugin for timestamp comparison
	shouldProc, reason, err := shouldProcess.Check(ctx, event.Path, naming.Outputs, event.IsModify)
	if err != nil {
		debugLogger.Decision(event.Path, "ERROR", fmt.Sprintf("should_process check failed: %v", err))
		return fmt.Errorf("should_process check failed: %w", err)
	}

	if !shouldProc {
		debugLogger.Decision(event.Path, "SKIP", reason)
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

	// Resolve command: use pattern-specific or fall back to global
	var command []string
	if event.Pattern != nil && event.Pattern.HasCommand() {
		command = event.Pattern.Command.AsSlice()
	} else {
		command = cfg.Command.AsSlice()
	}

	// Determine stack name from pattern (for stack mode)
	var stackName string
	if event.Pattern != nil && event.Pattern.Stack != "" {
		stackName = event.Pattern.Stack
	} else if cfg.Stacks.Default != "" {
		stackName = cfg.Stacks.Default
	}

	// Enqueue job with target type - output paths will be calculated at execution time
	job := &queue.Job{
		InputPath:  event.Path,
		TargetType: targetType,
		IsModify:   event.IsModify,
		GroupKey:   groupKey,
		StackName:  stackName,
		Command:    command,
	}

	// Apply instance tagging and default priority from options
	if len(opts) > 0 {
		if opts[0].instanceID != "" {
			job.InstanceID = opts[0].instanceID
		}
		if opts[0].defaultPriority != 0 {
			job.Priority = opts[0].defaultPriority
		}
	}

	if err := store.Enqueue(ctx, job); err != nil {
		return fmt.Errorf("failed to enqueue: %w", err)
	}

	debugLogger.Decision(event.Path, "QUEUED", fmt.Sprintf("targetType=%s, groupKey=%s, stackName=%s, reason=%s", targetType, groupKey, stackName, reason))
	if stackName != "" {
		logger.Printf("Queued: %s (target_type=%s, stack=%s, reason: %s)", event.Path, targetType, stackName, reason)
	} else {
		logger.Printf("Queued: %s (target_type=%s, reason: %s)", event.Path, targetType, reason)
	}
	return nil
}

// resolveInstanceID returns the instance ID to use, deriving from config file basename if not specified
func resolveInstanceID(explicit string, configFile string) string {
	if explicit != "" {
		return explicit
	}
	if configFile != "" {
		base := filepath.Base(configFile)
		ext := filepath.Ext(base)
		if ext != "" {
			return base[:len(base)-len(ext)]
		}
		return base
	}
	return "filehook"
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
