package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dandriscoll/filehook/internal/config"
	"github.com/dandriscoll/filehook/internal/debug"
	"github.com/dandriscoll/filehook/internal/output"
	"github.com/dandriscoll/filehook/internal/queue"
)

// silentLogger returns a logger that discards all output
func silentLogger() *output.Logger {
	return output.NewSilentLogger()
}

// mockStore implements queue.Store for testing
type mockStore struct {
	jobs           map[string]*queue.Job
	pendingByStack map[string][]*queue.Job
	stackState     *queue.StackState
	stackStats     map[string]*queue.StackStats
}

func newMockStore() *mockStore {
	return &mockStore{
		jobs:           make(map[string]*queue.Job),
		pendingByStack: make(map[string][]*queue.Job),
		stackState:     &queue.StackState{},
		stackStats:     make(map[string]*queue.StackStats),
	}
}

func (m *mockStore) Initialize(ctx context.Context) error { return nil }
func (m *mockStore) Close() error                         { return nil }

func (m *mockStore) Enqueue(ctx context.Context, job *queue.Job) error {
	if job.ID == "" {
		job.ID = "job-" + job.InputPath
	}
	job.Status = queue.JobStatusPending
	m.jobs[job.ID] = job
	m.pendingByStack[job.StackName] = append(m.pendingByStack[job.StackName], job)
	return nil
}

func (m *mockStore) Dequeue(ctx context.Context) (*queue.Job, error) {
	for _, jobs := range m.pendingByStack {
		for i, job := range jobs {
			if job.Status == queue.JobStatusPending {
				job.Status = queue.JobStatusRunning
				now := time.Now()
				job.StartedAt = &now
				m.pendingByStack[job.StackName] = append(jobs[:i], jobs[i+1:]...)
				return job, nil
			}
		}
	}
	return nil, nil
}

func (m *mockStore) DequeueForGroup(ctx context.Context, groupKey string) (*queue.Job, error) {
	return nil, nil
}

func (m *mockStore) Complete(ctx context.Context, jobID string, result *queue.JobResult) error {
	if job, ok := m.jobs[jobID]; ok {
		job.Status = queue.JobStatusCompleted
	}
	return nil
}

func (m *mockStore) Fail(ctx context.Context, jobID string, result *queue.JobResult) error {
	if job, ok := m.jobs[jobID]; ok {
		job.Status = queue.JobStatusFailed
	}
	return nil
}

func (m *mockStore) Get(ctx context.Context, jobID string) (*queue.Job, error) {
	return m.jobs[jobID], nil
}

func (m *mockStore) GetByInputPath(ctx context.Context, inputPath string) (*queue.Job, error) {
	return nil, nil
}

func (m *mockStore) ListPending(ctx context.Context, limit int) ([]queue.JobSummary, error) {
	return nil, nil
}

func (m *mockStore) ListFailed(ctx context.Context, limit int) ([]queue.JobSummary, error) {
	return nil, nil
}

func (m *mockStore) ListRunning(ctx context.Context) ([]queue.JobSummary, error) {
	return nil, nil
}

func (m *mockStore) ListRecentlyCompleted(ctx context.Context, limit int) ([]queue.JobSummary, error) {
	return nil, nil
}

func (m *mockStore) GetStats(ctx context.Context) (*queue.QueueStats, error) {
	return &queue.QueueStats{}, nil
}

func (m *mockStore) GetDistinctGroups(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (m *mockStore) Retry(ctx context.Context, jobID string) error {
	return nil
}

func (m *mockStore) RetryAll(ctx context.Context) (int, error) {
	return 0, nil
}

func (m *mockStore) HasPendingOrRunning(ctx context.Context, inputPath string) (bool, error) {
	return false, nil
}

func (m *mockStore) HasPending(ctx context.Context, inputPath string) (bool, error) {
	return false, nil
}

func (m *mockStore) CleanupStaleRunning(ctx context.Context) (int, error) {
	return 0, nil
}

func (m *mockStore) GetStackState(ctx context.Context) (*queue.StackState, error) {
	return m.stackState, nil
}

func (m *mockStore) SetCurrentStack(ctx context.Context, stackName string, switchDurationMs int64) error {
	m.stackState.CurrentStack = stackName
	now := time.Now()
	m.stackState.LastSwitchAt = &now
	m.stackState.LastSwitchDurationMs = switchDurationMs
	return nil
}

func (m *mockStore) UpdateStackJobStats(ctx context.Context, stackName string, jobDurationMs int64) error {
	if _, ok := m.stackStats[stackName]; !ok {
		m.stackStats[stackName] = &queue.StackStats{StackName: stackName}
	}
	m.stackStats[stackName].JobCount++
	m.stackStats[stackName].TotalJobDurationMs += jobDurationMs
	return nil
}

func (m *mockStore) GetPendingCountByStack(ctx context.Context) (map[string]int, error) {
	counts := make(map[string]int)
	for stack, jobs := range m.pendingByStack {
		pending := 0
		for _, job := range jobs {
			if job.Status == queue.JobStatusPending {
				pending++
			}
		}
		if pending > 0 {
			counts[stack] = pending
		}
	}
	return counts, nil
}

func (m *mockStore) DequeueForStack(ctx context.Context, stackName string) (*queue.Job, error) {
	jobs := m.pendingByStack[stackName]
	for i, job := range jobs {
		if job.Status == queue.JobStatusPending {
			job.Status = queue.JobStatusRunning
			now := time.Now()
			job.StartedAt = &now
			m.pendingByStack[stackName] = append(jobs[:i], jobs[i+1:]...)
			return job, nil
		}
	}
	return nil, nil
}

func (m *mockStore) GetStackStats(ctx context.Context) ([]queue.StackStats, error) {
	var stats []queue.StackStats
	for _, s := range m.stackStats {
		stats = append(stats, *s)
	}
	return stats, nil
}

// Priority management methods (stubs for testing)
func (m *mockStore) SetPriority(ctx context.Context, jobID string, priority int) error {
	if job, ok := m.jobs[jobID]; ok {
		job.Priority = priority
	}
	return nil
}

func (m *mockStore) GetMaxPriority(ctx context.Context) (int, error) {
	return 0, nil
}

func (m *mockStore) GetMinPriority(ctx context.Context) (int, error) {
	return 0, nil
}

func (m *mockStore) GetNextPendingJob(ctx context.Context) (*queue.Job, error) {
	return nil, nil
}

// Process tracking methods (stubs for testing)
func (m *mockStore) RegisterProcess(ctx context.Context, info *queue.ProcessInfo) error {
	return nil
}

func (m *mockStore) UnregisterProcess(ctx context.Context, pid int) error {
	return nil
}

func (m *mockStore) GetActiveProcess(ctx context.Context) (*queue.ProcessInfo, error) {
	return nil, nil
}

func (m *mockStore) GetActiveProcesses(ctx context.Context) ([]queue.ProcessInfo, error) {
	return nil, nil
}

func (m *mockStore) GetSchedulerProcess(ctx context.Context) (*queue.ProcessInfo, error) {
	return nil, nil
}

func (m *mockStore) UpdateHeartbeat(ctx context.Context, pid int) error {
	return nil
}

func (m *mockStore) DequeueWithClaim(ctx context.Context, claimerPID int) (*queue.Job, error) {
	return m.Dequeue(ctx)
}

func (m *mockStore) DequeueForStackWithClaim(ctx context.Context, stackName string, claimerPID int) (*queue.Job, error) {
	return m.DequeueForStack(ctx, stackName)
}

func (m *mockStore) CleanupStaleRunningForPID(ctx context.Context, pid int) (int, error) {
	return 0, nil
}

func (m *mockStore) ListPendingByInstance(ctx context.Context, instanceID string, limit int) ([]queue.JobSummary, error) {
	return nil, nil
}

func (m *mockStore) SetPriorityByInstance(ctx context.Context, jobID string, instanceID string, priority int) error {
	return nil
}

func (m *mockStore) GetStatsByInstance(ctx context.Context, instanceID string) (*queue.QueueStats, error) {
	return &queue.QueueStats{}, nil
}

func TestStackSchedulerSelectNextStack(t *testing.T) {
	// Create a temporary directory for scripts
	tmpDir, err := os.MkdirTemp("", "filehook-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		ConfigDir: tmpDir,
		Stacks: config.StacksConfig{
			Definitions: []config.StackDefinition{
				{Name: "stack_a", SwitchScript: "switch-a.sh"},
				{Name: "stack_b", SwitchScript: "switch-b.sh"},
			},
		},
	}

	store := newMockStore()

	scheduler := &StackScheduler{
		cfg:          cfg,
		store:        store,
		currentStack: "",
	}

	tests := []struct {
		name         string
		currentStack string
		counts       map[string]int
		wantStack    string
	}{
		{
			name:         "no current stack, pick largest batch",
			currentStack: "",
			counts:       map[string]int{"stack_a": 5, "stack_b": 10},
			wantStack:    "stack_b",
		},
		{
			name:         "current stack has jobs, continue with it",
			currentStack: "stack_a",
			counts:       map[string]int{"stack_a": 2, "stack_b": 10},
			wantStack:    "stack_a",
		},
		{
			name:         "current stack empty, switch to largest",
			currentStack: "stack_a",
			counts:       map[string]int{"stack_b": 10},
			wantStack:    "stack_b",
		},
		{
			name:         "single stack with jobs",
			currentStack: "",
			counts:       map[string]int{"stack_a": 1},
			wantStack:    "stack_a",
		},
	}

	ctx := context.Background()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheduler.mu.Lock()
			scheduler.currentStack = tt.currentStack
			scheduler.mu.Unlock()

			gotStack, err := scheduler.selectNextStack(ctx, tt.counts)
			if err != nil {
				t.Fatalf("selectNextStack failed: %v", err)
			}

			if gotStack != tt.wantStack {
				t.Errorf("selectNextStack() = %q, want %q", gotStack, tt.wantStack)
			}
		})
	}
}

func TestStackSchedulerSwitchToStack(t *testing.T) {
	// Create a temporary directory for scripts
	tmpDir, err := os.MkdirTemp("", "filehook-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a simple switch script
	scriptPath := filepath.Join(tmpDir, "switch-a.sh")
	scriptContent := `#!/bin/bash
echo "Switching to stack_a"
exit 0
`
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to create script: %v", err)
	}

	cfg := &config.Config{
		ConfigDir: tmpDir,
		Stacks: config.StacksConfig{
			Definitions: []config.StackDefinition{
				{Name: "stack_a", SwitchScript: "switch-a.sh"},
			},
		},
	}

	store := newMockStore()
	debugLogger, _ := debug.New(tmpDir, false) // disabled logger
	defer debugLogger.Close()

	scheduler := &StackScheduler{
		cfg:          cfg,
		store:        store,
		currentStack: "",
		debugLogger:  debugLogger,
		logger:       silentLogger(),
	}

	ctx := context.Background()

	if err := scheduler.switchToStack(ctx, "stack_a"); err != nil {
		t.Fatalf("switchToStack failed: %v", err)
	}

	if scheduler.currentStack != "stack_a" {
		t.Errorf("currentStack = %q, want %q", scheduler.currentStack, "stack_a")
	}

	state, _ := store.GetStackState(ctx)
	if state.CurrentStack != "stack_a" {
		t.Errorf("store.CurrentStack = %q, want %q", state.CurrentStack, "stack_a")
	}
}

func TestStackSchedulerSwitchToStackFailure(t *testing.T) {
	// Create a temporary directory for scripts
	tmpDir, err := os.MkdirTemp("", "filehook-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a failing switch script
	scriptPath := filepath.Join(tmpDir, "switch-fail.sh")
	scriptContent := `#!/bin/bash
echo "Switch failed!" >&2
exit 1
`
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to create script: %v", err)
	}

	cfg := &config.Config{
		ConfigDir: tmpDir,
		Stacks: config.StacksConfig{
			Definitions: []config.StackDefinition{
				{Name: "stack_fail", SwitchScript: "switch-fail.sh"},
			},
		},
	}

	store := newMockStore()
	debugLogger, _ := debug.New(tmpDir, false) // disabled logger
	defer debugLogger.Close()

	scheduler := &StackScheduler{
		cfg:          cfg,
		store:        store,
		currentStack: "",
		debugLogger:  debugLogger,
		logger:       silentLogger(),
	}

	ctx := context.Background()

	err = scheduler.switchToStack(ctx, "stack_fail")
	if err == nil {
		t.Error("switchToStack should have failed")
	}

	// Current stack should not have changed
	if scheduler.currentStack != "" {
		t.Errorf("currentStack = %q after failure, want empty", scheduler.currentStack)
	}
}

func TestStackSchedulerCurrentStack(t *testing.T) {
	scheduler := &StackScheduler{
		currentStack: "stack_a",
	}

	if got := scheduler.CurrentStack(); got != "stack_a" {
		t.Errorf("CurrentStack() = %q, want %q", got, "stack_a")
	}

	scheduler.mu.Lock()
	scheduler.currentStack = "stack_b"
	scheduler.mu.Unlock()

	if got := scheduler.CurrentStack(); got != "stack_b" {
		t.Errorf("CurrentStack() = %q, want %q", got, "stack_b")
	}
}

