package cli

import (
	"context"
	"fmt"

	"github.com/dandriscoll/filehook/internal/output"
	"github.com/dandriscoll/filehook/internal/queue"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show queue length, active workers, and last errors",
	RunE:  runStatus,
}

func init() {
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
