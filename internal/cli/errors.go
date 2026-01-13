package cli

import (
	"context"
	"fmt"

	"github.com/anthropics/filehook/internal/output"
	"github.com/anthropics/filehook/internal/queue"
	"github.com/spf13/cobra"
)

var (
	errorsLimit int
)

var errorsCmd = &cobra.Command{
	Use:   "errors",
	Short: "Error management commands",
}

var errorsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List failed jobs with ids and summaries",
	RunE:  runErrorsList,
}

var errorsShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show details and logs for a failed job",
	Args:  cobra.ExactArgs(1),
	RunE:  runErrorsShow,
}

func init() {
	rootCmd.AddCommand(errorsCmd)
	errorsCmd.AddCommand(errorsListCmd)
	errorsCmd.AddCommand(errorsShowCmd)

	errorsListCmd.Flags().IntVarP(&errorsLimit, "limit", "n", 50, "maximum number of items to show")
}

func runErrorsList(cmd *cobra.Command, args []string) error {
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

	failed, err := store.ListFailed(ctx, errorsLimit)
	if err != nil {
		return fmt.Errorf("failed to list failed jobs: %w", err)
	}

	formatter := output.New(isJSONOutput())
	return formatter.PrintJobSummaries(failed, "Failed jobs")
}

func runErrorsShow(cmd *cobra.Command, args []string) error {
	jobID := args[0]

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

	// Try to find job by full ID or prefix
	job, err := store.Get(ctx, jobID)
	if err != nil {
		return fmt.Errorf("failed to get job: %w", err)
	}
	if job == nil {
		return fmt.Errorf("job %q not found", jobID)
	}

	formatter := output.New(isJSONOutput())
	return formatter.PrintJob(job)
}
