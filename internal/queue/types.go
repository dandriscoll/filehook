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

// Job represents a file processing job
type Job struct {
	ID          string     `json:"id"`
	InputPath   string     `json:"input_path"`
	OutputPaths []string   `json:"output_paths"` // Calculated at execution time, not stored in DB
	TargetType  string     `json:"target_type,omitempty"`
	IsModify    bool       `json:"is_modify,omitempty"` // True if this was a file modification event
	Status      JobStatus  `json:"status"`
	GroupKey    string     `json:"group_key,omitempty"`
	StackName   string     `json:"stack_name,omitempty"` // Which stack this job requires (for stack mode)
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
	ID        string    `json:"id"`
	InputPath string    `json:"input_path"`
	Status    JobStatus `json:"status"`
	GroupKey  string    `json:"group_key,omitempty"`
	StackName string    `json:"stack_name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Error     string    `json:"error,omitempty"`
}

// ToSummary converts a Job to a JobSummary
func (j *Job) ToSummary() JobSummary {
	return JobSummary{
		ID:        j.ID,
		InputPath: j.InputPath,
		Status:    j.Status,
		GroupKey:  j.GroupKey,
		StackName: j.StackName,
		CreatedAt: j.CreatedAt,
		Error:     j.Error,
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
