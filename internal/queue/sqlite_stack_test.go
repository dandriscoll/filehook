package queue

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestStore(t *testing.T) (*SQLiteStore, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "filehook-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	store, err := NewSQLiteStore(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		store.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to initialize store: %v", err)
	}

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

func TestEnqueueWithStackName(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	job := &Job{
		InputPath: "/test/file1.txt",
		StackName: "stack_a",
		Status:    JobStatusPending,
	}

	if err := store.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Verify the job was stored with stack name
	retrieved, err := store.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.StackName != "stack_a" {
		t.Errorf("Job.StackName = %q, want %q", retrieved.StackName, "stack_a")
	}
}

func TestDequeueForStack(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Enqueue jobs for different stacks
	jobs := []*Job{
		{InputPath: "/test/file1.txt", StackName: "stack_a"},
		{InputPath: "/test/file2.txt", StackName: "stack_a"},
		{InputPath: "/test/file3.txt", StackName: "stack_b"},
		{InputPath: "/test/file4.txt", StackName: ""},
	}

	for _, job := range jobs {
		if err := store.Enqueue(ctx, job); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	// Dequeue from stack_a
	job, err := store.DequeueForStack(ctx, "stack_a")
	if err != nil {
		t.Fatalf("DequeueForStack failed: %v", err)
	}
	if job == nil {
		t.Fatal("DequeueForStack returned nil job")
	}
	if job.StackName != "stack_a" {
		t.Errorf("DequeueForStack returned job with stack %q, want %q", job.StackName, "stack_a")
	}
	if job.Status != JobStatusRunning {
		t.Errorf("DequeueForStack returned job with status %q, want %q", job.Status, JobStatusRunning)
	}

	// Dequeue from stack_b
	job, err = store.DequeueForStack(ctx, "stack_b")
	if err != nil {
		t.Fatalf("DequeueForStack failed: %v", err)
	}
	if job == nil {
		t.Fatal("DequeueForStack returned nil job for stack_b")
	}
	if job.StackName != "stack_b" {
		t.Errorf("DequeueForStack returned job with stack %q, want %q", job.StackName, "stack_b")
	}

	// Dequeue from empty stack (no stack name)
	job, err = store.DequeueForStack(ctx, "")
	if err != nil {
		t.Fatalf("DequeueForStack failed: %v", err)
	}
	if job == nil {
		t.Fatal("DequeueForStack returned nil job for empty stack")
	}
	if job.StackName != "" {
		t.Errorf("DequeueForStack returned job with stack %q, want empty", job.StackName)
	}

	// Stack_b should be empty now
	job, err = store.DequeueForStack(ctx, "stack_b")
	if err != nil {
		t.Fatalf("DequeueForStack failed: %v", err)
	}
	if job != nil {
		t.Errorf("DequeueForStack returned non-nil job for empty stack_b: %v", job)
	}
}

func TestGetPendingCountByStack(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Enqueue jobs for different stacks
	jobs := []*Job{
		{InputPath: "/test/file1.txt", StackName: "stack_a"},
		{InputPath: "/test/file2.txt", StackName: "stack_a"},
		{InputPath: "/test/file3.txt", StackName: "stack_a"},
		{InputPath: "/test/file4.txt", StackName: "stack_b"},
		{InputPath: "/test/file5.txt", StackName: "stack_b"},
		{InputPath: "/test/file6.txt", StackName: ""},
	}

	for _, job := range jobs {
		if err := store.Enqueue(ctx, job); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	counts, err := store.GetPendingCountByStack(ctx)
	if err != nil {
		t.Fatalf("GetPendingCountByStack failed: %v", err)
	}

	if counts["stack_a"] != 3 {
		t.Errorf("counts[stack_a] = %d, want 3", counts["stack_a"])
	}
	if counts["stack_b"] != 2 {
		t.Errorf("counts[stack_b] = %d, want 2", counts["stack_b"])
	}
	if counts[""] != 1 {
		t.Errorf("counts[\"\"] = %d, want 1", counts[""])
	}

	// Dequeue one from stack_a and check counts again
	_, err = store.DequeueForStack(ctx, "stack_a")
	if err != nil {
		t.Fatalf("DequeueForStack failed: %v", err)
	}

	counts, err = store.GetPendingCountByStack(ctx)
	if err != nil {
		t.Fatalf("GetPendingCountByStack failed: %v", err)
	}

	if counts["stack_a"] != 2 {
		t.Errorf("counts[stack_a] after dequeue = %d, want 2", counts["stack_a"])
	}
}

func TestStackState(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Initial state should be empty
	state, err := store.GetStackState(ctx)
	if err != nil {
		t.Fatalf("GetStackState failed: %v", err)
	}
	if state.CurrentStack != "" {
		t.Errorf("Initial CurrentStack = %q, want empty", state.CurrentStack)
	}

	// Set current stack
	if err := store.SetCurrentStack(ctx, "stack_a", 5000); err != nil {
		t.Fatalf("SetCurrentStack failed: %v", err)
	}

	// Verify state
	state, err = store.GetStackState(ctx)
	if err != nil {
		t.Fatalf("GetStackState failed: %v", err)
	}
	if state.CurrentStack != "stack_a" {
		t.Errorf("CurrentStack = %q, want %q", state.CurrentStack, "stack_a")
	}
	if state.LastSwitchDurationMs != 5000 {
		t.Errorf("LastSwitchDurationMs = %d, want 5000", state.LastSwitchDurationMs)
	}
	if state.LastSwitchAt == nil {
		t.Error("LastSwitchAt is nil, want non-nil")
	}

	// Update to different stack
	if err := store.SetCurrentStack(ctx, "stack_b", 3000); err != nil {
		t.Fatalf("SetCurrentStack failed: %v", err)
	}

	state, err = store.GetStackState(ctx)
	if err != nil {
		t.Fatalf("GetStackState failed: %v", err)
	}
	if state.CurrentStack != "stack_b" {
		t.Errorf("CurrentStack = %q, want %q", state.CurrentStack, "stack_b")
	}
	if state.LastSwitchDurationMs != 3000 {
		t.Errorf("LastSwitchDurationMs = %d, want 3000", state.LastSwitchDurationMs)
	}
}

func TestStackStats(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Initially empty
	stats, err := store.GetStackStats(ctx)
	if err != nil {
		t.Fatalf("GetStackStats failed: %v", err)
	}
	if len(stats) != 0 {
		t.Errorf("Initial stats length = %d, want 0", len(stats))
	}

	// Record some job completions
	if err := store.UpdateStackJobStats(ctx, "stack_a", 1000); err != nil {
		t.Fatalf("UpdateStackJobStats failed: %v", err)
	}
	if err := store.UpdateStackJobStats(ctx, "stack_a", 2000); err != nil {
		t.Fatalf("UpdateStackJobStats failed: %v", err)
	}
	if err := store.UpdateStackJobStats(ctx, "stack_b", 500); err != nil {
		t.Fatalf("UpdateStackJobStats failed: %v", err)
	}

	// Record some stack switches
	if err := store.SetCurrentStack(ctx, "stack_a", 5000); err != nil {
		t.Fatalf("SetCurrentStack failed: %v", err)
	}
	if err := store.SetCurrentStack(ctx, "stack_b", 3000); err != nil {
		t.Fatalf("SetCurrentStack failed: %v", err)
	}

	// Verify stats
	stats, err = store.GetStackStats(ctx)
	if err != nil {
		t.Fatalf("GetStackStats failed: %v", err)
	}

	// Find stack_a stats
	var stackAStats, stackBStats *StackStats
	for i := range stats {
		if stats[i].StackName == "stack_a" {
			stackAStats = &stats[i]
		} else if stats[i].StackName == "stack_b" {
			stackBStats = &stats[i]
		}
	}

	if stackAStats == nil {
		t.Fatal("stack_a stats not found")
	}
	if stackAStats.JobCount != 2 {
		t.Errorf("stack_a JobCount = %d, want 2", stackAStats.JobCount)
	}
	if stackAStats.TotalJobDurationMs != 3000 {
		t.Errorf("stack_a TotalJobDurationMs = %d, want 3000", stackAStats.TotalJobDurationMs)
	}
	if stackAStats.AvgJobDurationMs != 1500 {
		t.Errorf("stack_a AvgJobDurationMs = %d, want 1500", stackAStats.AvgJobDurationMs)
	}
	if stackAStats.SwitchCount != 1 {
		t.Errorf("stack_a SwitchCount = %d, want 1", stackAStats.SwitchCount)
	}

	if stackBStats == nil {
		t.Fatal("stack_b stats not found")
	}
	if stackBStats.JobCount != 1 {
		t.Errorf("stack_b JobCount = %d, want 1", stackBStats.JobCount)
	}
	if stackBStats.SwitchCount != 1 {
		t.Errorf("stack_b SwitchCount = %d, want 1", stackBStats.SwitchCount)
	}
}

func TestListByStatusIncludesStackName(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	job := &Job{
		InputPath: "/test/file1.txt",
		StackName: "stack_a",
	}
	if err := store.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	summaries, err := store.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("ListPending failed: %v", err)
	}

	if len(summaries) != 1 {
		t.Fatalf("ListPending returned %d jobs, want 1", len(summaries))
	}

	if summaries[0].StackName != "stack_a" {
		t.Errorf("JobSummary.StackName = %q, want %q", summaries[0].StackName, "stack_a")
	}
}

func TestJobSummaryIncludesStackName(t *testing.T) {
	job := &Job{
		ID:        "test-id",
		InputPath: "/test/file.txt",
		Status:    JobStatusPending,
		GroupKey:  "group1",
		StackName: "stack_a",
		CreatedAt: time.Now(),
	}

	summary := job.ToSummary()

	if summary.StackName != "stack_a" {
		t.Errorf("ToSummary().StackName = %q, want %q", summary.StackName, "stack_a")
	}
}

func TestDequeueOrderWithinStack(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Enqueue jobs in order
	for i := 1; i <= 3; i++ {
		job := &Job{
			InputPath: filepath.Join("/test", "file"+string(rune('0'+i))+".txt"),
			StackName: "stack_a",
		}
		if err := store.Enqueue(ctx, job); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
		// Small delay to ensure different timestamps
		time.Sleep(10 * time.Millisecond)
	}

	// Dequeue should return oldest first
	job1, _ := store.DequeueForStack(ctx, "stack_a")
	job2, _ := store.DequeueForStack(ctx, "stack_a")
	job3, _ := store.DequeueForStack(ctx, "stack_a")

	if job1.InputPath != "/test/file1.txt" {
		t.Errorf("First dequeue = %q, want file1.txt", job1.InputPath)
	}
	if job2.InputPath != "/test/file2.txt" {
		t.Errorf("Second dequeue = %q, want file2.txt", job2.InputPath)
	}
	if job3.InputPath != "/test/file3.txt" {
		t.Errorf("Third dequeue = %q, want file3.txt", job3.InputPath)
	}
}
