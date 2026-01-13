package cli

import (
	"context"
	"fmt"

	"github.com/anthropics/filehook/internal/output"
	"github.com/anthropics/filehook/internal/queue"
	"github.com/spf13/cobra"
)

var retryCmd = &cobra.Command{
	Use:   "retry <id|all>",
	Short: "Re-enqueue failed jobs",
	Args:  cobra.ExactArgs(1),
	RunE:  runRetry,
}

func init() {
	rootCmd.AddCommand(retryCmd)
}

func runRetry(cmd *cobra.Command, args []string) error {
	target := args[0]

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

	formatter := output.New(isJSONOutput())

	if target == "all" {
		count, err := store.RetryAll(ctx)
		if err != nil {
			return fmt.Errorf("failed to retry all: %w", err)
		}
		return formatter.PrintMessage(fmt.Sprintf("Re-enqueued %d failed jobs", count))
	}

	if err := store.Retry(ctx, target); err != nil {
		return err
	}

	return formatter.PrintMessage(fmt.Sprintf("Re-enqueued job %s", target))
}
