package cli

import (
	"context"
	"fmt"

	"github.com/dandriscoll/filehook/internal/output"
	"github.com/dandriscoll/filehook/internal/queue"
	"github.com/spf13/cobra"
)

var (
	queueLimit int
)

var queueCmd = &cobra.Command{
	Use:   "queue",
	Short: "Queue management commands",
}

var queueListCmd = &cobra.Command{
	Use:   "list",
	Short: "List queued items",
	RunE:  runQueueList,
}

func init() {
	rootCmd.AddCommand(queueCmd)
	queueCmd.AddCommand(queueListCmd)

	queueListCmd.Flags().IntVarP(&queueLimit, "limit", "n", 50, "maximum number of items to show")
}

func runQueueList(cmd *cobra.Command, args []string) error {
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

	pending, err := store.ListPending(ctx, queueLimit)
	if err != nil {
		return fmt.Errorf("failed to list pending jobs: %w", err)
	}

	formatter := output.New(isJSONOutput())
	return formatter.PrintJobSummaries(pending, "Pending jobs")
}
