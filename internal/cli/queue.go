package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/dandriscoll/filehook/internal/output"
	"github.com/dandriscoll/filehook/internal/queue"
	"github.com/spf13/cobra"
)

var (
	queueLimit    int
	queueInstance string
)

var queueCmd = &cobra.Command{
	Use:   "queue",
	Short: "Queue management commands",
}

var queueListCmd = &cobra.Command{
	Use:   "list",
	Short: "List queued items (ordered by priority)",
	RunE:  runQueueList,
}

var queueMoveCmd = &cobra.Command{
	Use:   "move <job-id> <position>",
	Short: "Move a job in the queue priority",
	Long: `Move a job to a different position in the queue priority.

Positions:
  top      Move to highest priority (will be processed next)
  up       Increase priority by 1
  down     Decrease priority by 1
  bottom   Move to lowest priority

You can also specify an absolute priority number.`,
	Args: cobra.ExactArgs(2),
	RunE: runQueueMove,
}

var queueBumpCmd = &cobra.Command{
	Use:   "bump <pattern>",
	Short: "Bump matching pending jobs to the front of the queue",
	Long: `Move all pending jobs whose input path matches the pattern to the front of the queue.

The pattern is matched against:
  - The full input path (exact match)
  - The filename/basename (exact match)
  - A file glob (matched against both full path and basename)

Examples:
  filehook queue bump myfile.txt
  filehook queue bump /path/to/file.txt
  filehook queue bump "*.jpg"`,
	Args: cobra.ExactArgs(1),
	RunE: runQueueBump,
}

var queuePriorityCmd = &cobra.Command{
	Use:   "priority <job-id> <priority>",
	Short: "Set the priority of a job",
	Long:  `Set the absolute priority of a pending job. Higher values = higher priority.`,
	Args:  cobra.ExactArgs(2),
	RunE:  runQueuePriority,
}

func init() {
	rootCmd.AddCommand(queueCmd)
	queueCmd.AddCommand(queueListCmd)
	queueCmd.AddCommand(queueMoveCmd)
	queueCmd.AddCommand(queueBumpCmd)
	queueCmd.AddCommand(queuePriorityCmd)

	queueListCmd.Flags().IntVarP(&queueLimit, "limit", "l", 50, "maximum number of items to show")
	queueListCmd.Flags().StringVar(&queueInstance, "instance", "", "filter by submitting instance")
	queueMoveCmd.Flags().StringVar(&queueInstance, "instance", "", "only modify jobs from this instance")
	queueBumpCmd.Flags().StringVar(&queueInstance, "instance", "", "filter by submitting instance")
	queuePriorityCmd.Flags().StringVar(&queueInstance, "instance", "", "only modify jobs from this instance")
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

	var pending []queue.JobSummary
	if queueInstance != "" {
		pending, err = store.ListPendingByInstance(ctx, queueInstance, queueLimit)
	} else {
		pending, err = store.ListPending(ctx, queueLimit)
	}
	if err != nil {
		return fmt.Errorf("failed to list pending jobs: %w", err)
	}

	title := "Pending jobs"
	if queueInstance != "" {
		title = fmt.Sprintf("Pending jobs (instance: %s)", queueInstance)
	}
	formatter := output.New(isJSONOutput())
	return formatter.PrintJobSummaries(pending, title)
}

func runQueueMove(cmd *cobra.Command, args []string) error {
	jobID := args[0]
	position := args[1]

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

	// Get the current job to find its priority
	job, err := store.Get(ctx, jobID)
	if err != nil {
		return fmt.Errorf("failed to get job: %w", err)
	}
	if job == nil {
		return fmt.Errorf("job %q not found", jobID)
	}
	if job.Status != queue.JobStatusPending {
		return fmt.Errorf("job %q is not pending (status: %s)", jobID, job.Status)
	}

	var newPriority int

	switch position {
	case "top":
		maxPriority, err := store.GetMaxPriority(ctx)
		if err != nil {
			return fmt.Errorf("failed to get max priority: %w", err)
		}
		newPriority = maxPriority + 1

	case "up":
		newPriority = job.Priority + 1

	case "down":
		newPriority = job.Priority - 1

	case "bottom":
		minPriority, err := store.GetMinPriority(ctx)
		if err != nil {
			return fmt.Errorf("failed to get min priority: %w", err)
		}
		newPriority = minPriority - 1

	default:
		// Try to parse as an integer
		p, err := strconv.Atoi(position)
		if err != nil {
			return fmt.Errorf("invalid position %q: use top, up, down, bottom, or a number", position)
		}
		newPriority = p
	}

	if queueInstance != "" {
		if err := store.SetPriorityByInstance(ctx, jobID, queueInstance, newPriority); err != nil {
			return fmt.Errorf("failed to set priority: %w", err)
		}
	} else {
		if err := store.SetPriority(ctx, jobID, newPriority); err != nil {
			return fmt.Errorf("failed to set priority: %w", err)
		}
	}

	formatter := output.New(isJSONOutput())
	return formatter.PrintMessage(fmt.Sprintf("Job %s priority set to %d", jobID[:8], newPriority))
}

func runQueuePriority(cmd *cobra.Command, args []string) error {
	jobID := args[0]
	priorityStr := args[1]

	priority, err := strconv.Atoi(priorityStr)
	if err != nil {
		return fmt.Errorf("invalid priority %q: must be a number", priorityStr)
	}

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

	if queueInstance != "" {
		if err := store.SetPriorityByInstance(ctx, jobID, queueInstance, priority); err != nil {
			return fmt.Errorf("failed to set priority: %w", err)
		}
	} else {
		if err := store.SetPriority(ctx, jobID, priority); err != nil {
			return fmt.Errorf("failed to set priority: %w", err)
		}
	}

	formatter := output.New(isJSONOutput())
	return formatter.PrintMessage(fmt.Sprintf("Job %s priority set to %d", jobID[:8], priority))
}

func runQueueBump(cmd *cobra.Command, args []string) error {
	pattern := args[0]

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

	bumped, err := store.BumpByPattern(ctx, pattern)
	if err != nil {
		return fmt.Errorf("failed to bump jobs: %w", err)
	}

	formatter := output.New(isJSONOutput())
	if len(bumped) == 0 {
		return formatter.PrintMessage(fmt.Sprintf("No pending jobs match %q", pattern))
	}

	if err := formatter.PrintMessage(fmt.Sprintf("Bumped %d job(s) to front of queue", len(bumped))); err != nil {
		return err
	}
	return formatter.PrintJobSummaries(bumped, "Bumped jobs")
}
