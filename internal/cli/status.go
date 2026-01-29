package cli

import (
	"context"
	"fmt"

	"github.com/dandriscoll/filehook/internal/config"
	"github.com/dandriscoll/filehook/internal/output"
	"github.com/dandriscoll/filehook/internal/queue"
	"github.com/spf13/cobra"
)

var showStacks bool

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show queue length, active workers, and last errors",
	RunE:  runStatus,
}

var stateCmd = &cobra.Command{
	Use:   "state",
	Short: "Show process state (for programmatic use)",
	Long: `Show the current state of filehook processing.

Returns a simple state indicator:
  not_running  - No filehook process is actively processing
  idle         - A filehook process is running but no work to do
  processing   - A filehook process is running and has work

Use --json for machine-readable output that includes:
  - state: The process state
  - process: Info about the running process (if any)
  - stats: Queue statistics
  - current_job: Currently running job (if any)
  - next_job: Next job to be processed (if any)
  - queue_length: Number of pending jobs`,
	RunE: runState,
}

func init() {
	statusCmd.Flags().BoolVar(&showStacks, "stacks", false, "show detailed stack information (for stack mode)")
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(stateCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	store, err := queue.NewSQLiteStore(cfg.StateDirectory())
	if err != nil {
		return fmt.Errorf("failed to open queue: %w", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize queue: %w", err)
	}

	stats, err := store.GetStats(ctx)
	if err != nil {
		return fmt.Errorf("failed to get stats: %w", err)
	}

	formatter := output.New(isJSONOutput())
	formatter.PrintStats(stats)

	// Show stack information if in stack mode
	if cfg.Concurrency.Mode == config.ConcurrencyStack || showStacks {
		if err := printStackStatus(ctx, cfg, store, formatter); err != nil {
			return err
		}
	}

	// Show running jobs if any
	running, err := store.ListRunning(ctx)
	if err != nil {
		return fmt.Errorf("failed to list running jobs: %w", err)
	}
	if len(running) > 0 {
		fmt.Println()
		formatter.PrintJobSummaries(running, "Running")
	}

	// Show recent failures if any
	failed, err := store.ListFailed(ctx, 5)
	if err != nil {
		return fmt.Errorf("failed to list failed jobs: %w", err)
	}
	if len(failed) > 0 {
		fmt.Println()
		formatter.PrintJobSummaries(failed, "Recent failures")
	}

	return nil
}

func printStackStatus(ctx context.Context, cfg *config.Config, store queue.Store, formatter *output.Formatter) error {
	fmt.Println()

	// Get current stack state
	state, err := store.GetStackState(ctx)
	if err != nil {
		return fmt.Errorf("failed to get stack state: %w", err)
	}

	if cfg.Concurrency.Mode == config.ConcurrencyStack {
		fmt.Println("Stack Mode: active")
	} else {
		fmt.Println("Stack Mode: inactive (use --stacks to show stats)")
	}

	if state.CurrentStack != "" {
		fmt.Printf("Current Stack: %s\n", state.CurrentStack)
	} else {
		fmt.Println("Current Stack: (none)")
	}

	// Get pending counts by stack
	counts, err := store.GetPendingCountByStack(ctx)
	if err != nil {
		return fmt.Errorf("failed to get pending counts by stack: %w", err)
	}

	if len(counts) > 0 {
		fmt.Println("\nPending by Stack:")
		for stack, count := range counts {
			stackLabel := stack
			if stackLabel == "" {
				stackLabel = "(no stack)"
			}
			suffix := ""
			if stack == state.CurrentStack {
				suffix = " (current)"
			}
			fmt.Printf("  %s: %d jobs%s\n", stackLabel, count, suffix)
		}
	}

	// Get stack statistics
	stackStats, err := store.GetStackStats(ctx)
	if err != nil {
		return fmt.Errorf("failed to get stack stats: %w", err)
	}

	if len(stackStats) > 0 {
		fmt.Println("\nStack Statistics:")
		for _, ss := range stackStats {
			avgJobSec := float64(ss.AvgJobDurationMs) / 1000.0
			avgSwitchSec := float64(ss.AvgSwitchDurationMs) / 1000.0
			fmt.Printf("  %s: %d jobs avg %.1fs, %d switches avg %.1fs\n",
				ss.StackName, ss.JobCount, avgJobSec, ss.SwitchCount, avgSwitchSec)
		}
	}

	return nil
}

func runState(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	store, err := queue.NewSQLiteStore(cfg.StateDirectory())
	if err != nil {
		return fmt.Errorf("failed to open queue: %w", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize queue: %w", err)
	}

	// Build the full status
	fullStatus := &queue.FullStatus{}

	// Get queue stats
	stats, err := store.GetStats(ctx)
	if err != nil {
		return fmt.Errorf("failed to get stats: %w", err)
	}
	fullStatus.Stats = stats
	fullStatus.QueueLength = stats.Pending

	// Get process info (multi-process aware)
	allProcesses, err := store.GetActiveProcesses(ctx)
	if err != nil {
		return fmt.Errorf("failed to get process info: %w", err)
	}

	// Populate scheduler and producers
	for _, p := range allProcesses {
		p := p
		if p.Role == queue.ProcessRoleScheduler {
			fullStatus.Scheduler = &p
		} else {
			fullStatus.Producers = append(fullStatus.Producers, p)
		}
	}

	// Backward compat: set Process to first active process
	var processInfo *queue.ProcessInfo
	if fullStatus.Scheduler != nil {
		processInfo = fullStatus.Scheduler
	} else if len(fullStatus.Producers) > 0 {
		processInfo = &fullStatus.Producers[0]
	} else if len(allProcesses) > 0 {
		processInfo = &allProcesses[0]
	}
	fullStatus.Process = processInfo

	// Get current running job (if any)
	running, err := store.ListRunning(ctx)
	if err != nil {
		return fmt.Errorf("failed to list running jobs: %w", err)
	}
	if len(running) > 0 {
		fullStatus.CurrentJob = &running[0]
	}

	// Get next pending job (if any)
	nextJob, err := store.GetNextPendingJob(ctx)
	if err != nil {
		return fmt.Errorf("failed to get next pending job: %w", err)
	}
	if nextJob != nil {
		summary := nextJob.ToSummary()
		fullStatus.NextJob = &summary
	}

	// Determine the state
	if processInfo == nil {
		fullStatus.State = queue.ProcessStateNotRunning
	} else if stats.Running > 0 || stats.Pending > 0 {
		fullStatus.State = queue.ProcessStateProcessing
	} else {
		fullStatus.State = queue.ProcessStateIdle
	}

	formatter := output.New(isJSONOutput())
	return formatter.PrintFullStatus(fullStatus)
}
