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

func init() {
	statusCmd.Flags().BoolVar(&showStacks, "stacks", false, "show detailed stack information (for stack mode)")
	rootCmd.AddCommand(statusCmd)
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
