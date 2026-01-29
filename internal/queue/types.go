package queue

import (
	"time"
)

// JobStatus represents the status of a job
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
)

// ProcessRole represents the role of a registered process
type ProcessRole string

const (
	ProcessRoleProducer  ProcessRole = "producer"
	ProcessRoleScheduler ProcessRole = "scheduler"
	ProcessRoleLegacy    ProcessRole = "legacy"
)

// Job represents a file processing job
type Job struct {
	ID          string     `json:"id"`
	InputPath   string     `json:"input_path"`
	OutputPaths []string   `json:"output_paths"` // Calculated at execution time, not stored in DB
	TargetType  string     `json:"target_type,omitempty"`
	IsModify    bool       `json:"is_modify,omitempty"` // True if this was a file modification event
	Status      JobStatus  `json:"status"`
	Priority    int        `json:"priority"`            // Higher priority = processed first (default 0)
	GroupKey    string     `json:"group_key,omitempty"`
	StackName   string     `json:"stack_name,omitempty"` // Which stack this job requires (for stack mode)
	InstanceID  string     `json:"instance_id,omitempty"`
	ClaimedBy   *int       `json:"claimed_by,omitempty"`
	Command     []string   `json:"command,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	ExitCode    *int       `json:"exit_code,omitempty"`
	DurationMs  *int64     `json:"duration_ms,omitempty"`
	Error       string     `json:"error,omitempty"`
	Stdout      string     `json:"stdout,omitempty"`
	Stderr      string     `json:"stderr,omitempty"`
}

// JobSummary is a brief representation of a job for listing
type JobSummary struct {
	ID          string     `json:"id"`
	InputPath   string     `json:"input_path"`
	Status      JobStatus  `json:"status"`
	Priority    int        `json:"priority"`
	GroupKey    string     `json:"group_key,omitempty"`
	StackName   string     `json:"stack_name,omitempty"`
	InstanceID  string     `json:"instance_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	DurationMs  *int64     `json:"duration_ms,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// ToSummary converts a Job to a JobSummary
func (j *Job) ToSummary() JobSummary {
	return JobSummary{
		ID:         j.ID,
		InputPath:  j.InputPath,
		Status:     j.Status,
		Priority:   j.Priority,
		GroupKey:   j.GroupKey,
		StackName:  j.StackName,
		InstanceID:  j.InstanceID,
		CreatedAt:   j.CreatedAt,
		CompletedAt: j.CompletedAt,
		DurationMs:  j.DurationMs,
		Error:       j.Error,
	}
}

// QueueStats holds queue statistics
type QueueStats struct {
	Pending   int `json:"pending"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Total     int `json:"total"`
}

// JobResult holds the result of a job execution
type JobResult struct {
	ExitCode   int
	Stdout     string
	Stderr     string
	DurationMs int64
	Error      error
}

// StackState represents the current stack state (for stack mode)
type StackState struct {
	CurrentStack         string
	LastSwitchAt         *time.Time
	LastSwitchDurationMs int64
}

// StackStats holds statistics for a single stack
type StackStats struct {
	StackName             string
	JobCount              int
	TotalJobDurationMs    int64
	AvgJobDurationMs      int64 // computed
	SwitchCount           int
	TotalSwitchDurationMs int64
	AvgSwitchDurationMs   int64 // computed
}

// ProcessState represents the state of the filehook process
type ProcessState string

const (
	ProcessStateNotRunning ProcessState = "not_running" // No filehook process is active
	ProcessStateIdle       ProcessState = "idle"        // Process is active but no work to do
	ProcessStateProcessing ProcessState = "processing"  // Process is active and processing/has pending work
)

// ProcessInfo holds information about a running filehook process
type ProcessInfo struct {
	PID         int         `json:"pid"`
	Command     string      `json:"command"` // "watch", "run", or "serve"
	Role        ProcessRole `json:"role"`
	InstanceID  string      `json:"instance_id,omitempty"`
	StartedAt   time.Time   `json:"started_at"`
	HeartbeatAt time.Time   `json:"heartbeat_at"`
}

// FullStatus holds complete status information for API consumers
type FullStatus struct {
	State              ProcessState   `json:"state"`
	Process            *ProcessInfo   `json:"process,omitempty"` // Kept for backward compat
	Scheduler          *ProcessInfo   `json:"scheduler,omitempty"`
	Producers          []ProcessInfo  `json:"producers,omitempty"`
	Stats              *QueueStats    `json:"stats"`
	CurrentJob         *JobSummary    `json:"current_job,omitempty"`          // Currently running job (if any)
	NextJob            *JobSummary    `json:"next_job,omitempty"`             // Next job to be processed (if any)
	QueueLength        int            `json:"queue_length"`                   // Number of pending jobs
	RecentlyCompleted  []JobSummary   `json:"recently_completed,omitempty"`   // Recently completed jobs
	RecentlyFailed     []JobSummary   `json:"recently_failed,omitempty"`      // Recently failed jobs
	PendingJobs        []JobSummary   `json:"pending_jobs,omitempty"`         // Pending jobs (first N)
	CurrentStack       string         `json:"current_stack,omitempty"`        // Current stack (stack mode)
	PendingByStack     map[string]int `json:"pending_by_stack,omitempty"`     // Pending counts by stack
}
