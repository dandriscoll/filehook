package queue

import "context"

// Store defines the interface for job queue storage
type Store interface {
	// Initialize sets up the store (creates tables, etc.)
	Initialize(ctx context.Context) error

	// Close closes the store
	Close() error

	// Enqueue adds a new job to the queue
	// Returns the job ID or error if duplicate pending/running job exists
	Enqueue(ctx context.Context, job *Job) error

	// Dequeue gets the next pending job and marks it as running
	// Returns nil if no jobs are available
	Dequeue(ctx context.Context) (*Job, error)

	// DequeueForGroup gets the next pending job for a specific group
	// Used by the sequential switching scheduler
	DequeueForGroup(ctx context.Context, groupKey string) (*Job, error)

	// Complete marks a job as completed with the given result
	Complete(ctx context.Context, jobID string, result *JobResult) error

	// Fail marks a job as failed with the given result
	Fail(ctx context.Context, jobID string, result *JobResult) error

	// Get retrieves a job by ID
	Get(ctx context.Context, jobID string) (*Job, error)

	// GetByInputPath retrieves a job by input path (pending or running)
	GetByInputPath(ctx context.Context, inputPath string) (*Job, error)

	// ListPending returns all pending jobs
	ListPending(ctx context.Context, limit int) ([]JobSummary, error)

	// ListFailed returns all failed jobs
	ListFailed(ctx context.Context, limit int) ([]JobSummary, error)

	// ListRunning returns all running jobs
	ListRunning(ctx context.Context) ([]JobSummary, error)

	// GetStats returns queue statistics
	GetStats(ctx context.Context) (*QueueStats, error)

	// GetDistinctGroups returns distinct group keys for pending jobs
	GetDistinctGroups(ctx context.Context) ([]string, error)

	// Retry re-enqueues a failed job
	Retry(ctx context.Context, jobID string) error

	// RetryAll re-enqueues all failed jobs
	RetryAll(ctx context.Context) (int, error)

	// HasPendingOrRunning checks if a job exists for the input path
	HasPendingOrRunning(ctx context.Context, inputPath string) (bool, error)

	// CleanupStaleRunning resets jobs that were running but never completed
	// (e.g., after a crash)
	CleanupStaleRunning(ctx context.Context) (int, error)

	// Stack mode methods

	// GetStackState returns the current stack state
	GetStackState(ctx context.Context) (*StackState, error)

	// SetCurrentStack updates the current stack and records switch duration
	SetCurrentStack(ctx context.Context, stackName string, switchDurationMs int64) error

	// UpdateStackJobStats records a job completion for stack statistics
	UpdateStackJobStats(ctx context.Context, stackName string, jobDurationMs int64) error

	// GetPendingCountByStack returns pending job counts grouped by stack
	GetPendingCountByStack(ctx context.Context) (map[string]int, error)

	// DequeueForStack gets the next pending job for a specific stack
	DequeueForStack(ctx context.Context, stackName string) (*Job, error)

	// GetStackStats returns statistics for all stacks
	GetStackStats(ctx context.Context) ([]StackStats, error)
}
