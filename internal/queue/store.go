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

	// ListRecentlyCompleted returns recently completed jobs ordered by completion time descending
	ListRecentlyCompleted(ctx context.Context, limit int) ([]JobSummary, error)

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

	// HasPending checks if a pending job exists for the input path
	HasPending(ctx context.Context, inputPath string) (bool, error)

	// CleanupStaleRunning resets jobs that were running but never completed
	// (e.g., after a crash)
	CleanupStaleRunning(ctx context.Context) (int, error)

	// Priority management methods

	// SetPriority sets the priority of a pending job
	SetPriority(ctx context.Context, jobID string, priority int) error

	// GetMaxPriority returns the maximum priority among pending jobs
	GetMaxPriority(ctx context.Context) (int, error)

	// GetMinPriority returns the minimum priority among pending jobs
	GetMinPriority(ctx context.Context) (int, error)

	// GetNextPendingJob returns the next job that would be dequeued (without marking it as running)
	GetNextPendingJob(ctx context.Context) (*Job, error)

	// Process tracking methods

	// RegisterProcess registers a running filehook process
	RegisterProcess(ctx context.Context, info *ProcessInfo) error

	// UnregisterProcess removes a process registration
	UnregisterProcess(ctx context.Context, pid int) error

	// GetActiveProcess returns the currently registered process, or nil if none
	GetActiveProcess(ctx context.Context) (*ProcessInfo, error)

	// GetActiveProcesses returns all live registered processes
	GetActiveProcesses(ctx context.Context) ([]ProcessInfo, error)

	// GetSchedulerProcess returns the live scheduler process, or nil if none
	GetSchedulerProcess(ctx context.Context) (*ProcessInfo, error)

	// UpdateHeartbeat updates the heartbeat timestamp for a process
	UpdateHeartbeat(ctx context.Context, pid int) error

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

	// Claim-aware dequeue methods (for central scheduler)

	// DequeueWithClaim dequeues the next pending job and sets claimed_by
	DequeueWithClaim(ctx context.Context, claimerPID int) (*Job, error)

	// DequeueForStackWithClaim dequeues the next pending job for a stack and sets claimed_by
	DequeueForStackWithClaim(ctx context.Context, stackName string, claimerPID int) (*Job, error)

	// CleanupStaleRunningForPID resets running jobs claimed by a specific dead PID
	CleanupStaleRunningForPID(ctx context.Context, pid int) (int, error)

	// Instance-filtered queries

	// ListPendingByInstance returns pending jobs filtered by instance ID
	ListPendingByInstance(ctx context.Context, instanceID string, limit int) ([]JobSummary, error)

	// SetPriorityByInstance sets priority only if the job belongs to the given instance
	SetPriorityByInstance(ctx context.Context, jobID string, instanceID string, priority int) error

	// GetStatsByInstance returns queue stats filtered by instance ID
	GetStatsByInstance(ctx context.Context, instanceID string) (*QueueStats, error)
}
