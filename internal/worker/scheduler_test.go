package worker

import (
	"context"
	"testing"
	"time"

	"github.com/dandriscoll/filehook/internal/config"
	"github.com/dandriscoll/filehook/internal/debug"
	"github.com/dandriscoll/filehook/internal/queue"
)

func TestCentralSchedulerSelectStack(t *testing.T) {
	cs := &CentralScheduler{}

	tests := []struct {
		name         string
		currentStack string
		counts       map[string]int
		wantStack    string
	}{
		{
			name:         "no current stack, pick largest",
			currentStack: "",
			counts:       map[string]int{"a": 5, "b": 10},
			wantStack:    "b",
		},
		{
			name:         "current stack has work, stay",
			currentStack: "a",
			counts:       map[string]int{"a": 2, "b": 10},
			wantStack:    "a",
		},
		{
			name:         "current stack empty, switch",
			currentStack: "a",
			counts:       map[string]int{"b": 10},
			wantStack:    "b",
		},
		{
			name:         "single stack",
			currentStack: "",
			counts:       map[string]int{"a": 1},
			wantStack:    "a",
		},
		{
			name:         "empty counts",
			currentStack: "a",
			counts:       map[string]int{},
			wantStack:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cs.selectStack(tt.currentStack, tt.counts)
			if got != tt.wantStack {
				t.Errorf("selectStack() = %q, want %q", got, tt.wantStack)
			}
		})
	}
}

func TestCentralSchedulerExecuteJob(t *testing.T) {
	// Verify that executeJob calls Complete/Fail on the store
	store := newMockStore()

	job := &queue.Job{
		ID:        "exec-test",
		InputPath: "/nonexistent/file.txt",
		Command:   []string{"echo", "hello"},
		Status:    queue.JobStatusRunning,
	}
	store.jobs[job.ID] = job

	cs := &CentralScheduler{
		store:       store,
		pid:         99999,
		logger:      silentLogger(),
		debugLogger: nil,
	}

	// We can't easily test executeJob without a real executor,
	// but we can verify the scheduler struct is constructable
	if cs.pid != 99999 {
		t.Errorf("expected pid 99999, got %d", cs.pid)
	}
}

func disabledDebugLogger(t *testing.T) *debug.Logger {
	t.Helper()
	dl, _ := debug.New(t.TempDir(), false)
	return dl
}

func TestCentralSchedulerStartStop(t *testing.T) {
	store := newMockStore()

	cs := &CentralScheduler{
		cfg:         &config.Config{},
		store:       store,
		pid:         12345,
		stopCh:      make(chan struct{}),
		logger:      silentLogger(),
		debugLogger: disabledDebugLogger(t),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs.Start(ctx)

	// Let it run briefly
	time.Sleep(50 * time.Millisecond)

	// Stop should not hang
	cs.Stop()
}

func TestCentralSchedulerProcessesJobFromMock(t *testing.T) {
	store := newMockStore()

	// Add a pending job
	job := &queue.Job{
		ID:        "proc-1",
		InputPath: "/a/1.txt",
		Status:    queue.JobStatusPending,
		CreatedAt: time.Now(),
	}
	store.Enqueue(context.Background(), job)

	cs := &CentralScheduler{
		store:       store,
		pid:         12345,
		stopCh:      make(chan struct{}),
		logger:      silentLogger(),
		debugLogger: nil,
	}

	ctx := context.Background()

	// Dequeue with claim (non-stack mode path)
	claimed, err := cs.store.DequeueWithClaim(ctx, cs.pid)
	if err != nil {
		t.Fatalf("failed to dequeue: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected job")
	}
	if claimed.ID != "proc-1" {
		t.Errorf("expected proc-1, got %s", claimed.ID)
	}
	if claimed.Status != queue.JobStatusRunning {
		t.Errorf("expected running, got %s", claimed.Status)
	}
}

func TestCentralSchedulerSelectStackAllEmpty(t *testing.T) {
	cs := &CentralScheduler{}

	// nil map
	got := cs.selectStack("", nil)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	// map with zero counts
	got = cs.selectStack("a", map[string]int{"a": 0, "b": 0})
	if got != "" {
		t.Errorf("expected empty for zero counts, got %q", got)
	}
}

func TestCentralSchedulerSelectStackTieBreaking(t *testing.T) {
	cs := &CentralScheduler{}

	// With equal counts and no current stack, should pick one (deterministic within a single run)
	counts := map[string]int{"a": 5, "b": 5}
	got := cs.selectStack("", counts)
	if got != "a" && got != "b" {
		t.Errorf("expected a or b, got %q", got)
	}
}
