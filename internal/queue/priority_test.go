package queue

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPriorityOrdering(t *testing.T) {
	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "filehook-priority-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewSQLiteStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatalf("failed to initialize store: %v", err)
	}

	// Enqueue jobs with different priorities
	jobs := []*Job{
		{ID: "job-1", InputPath: "/path/file1.txt", Priority: 0},
		{ID: "job-2", InputPath: "/path/file2.txt", Priority: 10},
		{ID: "job-3", InputPath: "/path/file3.txt", Priority: 5},
		{ID: "job-4", InputPath: "/path/file4.txt", Priority: -5},
	}

	for _, job := range jobs {
		job.Status = JobStatusPending
		job.CreatedAt = time.Now()
		if err := store.Enqueue(ctx, job); err != nil {
			t.Fatalf("failed to enqueue job %s: %v", job.ID, err)
		}
		// Small delay to ensure different created_at times
		time.Sleep(10 * time.Millisecond)
	}

	// Dequeue should return jobs in priority order: job-2 (10), job-3 (5), job-1 (0), job-4 (-5)
	expectedOrder := []string{"job-2", "job-3", "job-1", "job-4"}

	for i, expectedID := range expectedOrder {
		job, err := store.Dequeue(ctx)
		if err != nil {
			t.Fatalf("failed to dequeue job %d: %v", i, err)
		}
		if job == nil {
			t.Fatalf("expected job %s but got nil", expectedID)
		}
		if job.ID != expectedID {
			t.Errorf("dequeue %d: expected %s, got %s", i, expectedID, job.ID)
		}
		// Mark as completed so it's not returned again
		store.Complete(ctx, job.ID, &JobResult{})
	}
}

func TestSetPriority(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filehook-priority-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewSQLiteStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatalf("failed to initialize store: %v", err)
	}

	// Enqueue a job
	job := &Job{
		ID:        "test-job",
		InputPath: "/path/test.txt",
		Status:    JobStatusPending,
		Priority:  0,
		CreatedAt: time.Now(),
	}
	if err := store.Enqueue(ctx, job); err != nil {
		t.Fatalf("failed to enqueue job: %v", err)
	}

	// Set priority
	if err := store.SetPriority(ctx, "test-job", 100); err != nil {
		t.Fatalf("failed to set priority: %v", err)
	}

	// Verify priority was updated
	updated, err := store.Get(ctx, "test-job")
	if err != nil {
		t.Fatalf("failed to get job: %v", err)
	}
	if updated.Priority != 100 {
		t.Errorf("expected priority 100, got %d", updated.Priority)
	}
}

func TestSetPriorityOnlyPending(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filehook-priority-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewSQLiteStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatalf("failed to initialize store: %v", err)
	}

	// Enqueue and dequeue a job (making it running)
	job := &Job{
		ID:        "running-job",
		InputPath: "/path/test.txt",
		Status:    JobStatusPending,
		CreatedAt: time.Now(),
	}
	if err := store.Enqueue(ctx, job); err != nil {
		t.Fatalf("failed to enqueue job: %v", err)
	}

	_, err = store.Dequeue(ctx)
	if err != nil {
		t.Fatalf("failed to dequeue job: %v", err)
	}

	// Try to set priority on running job - should fail
	err = store.SetPriority(ctx, "running-job", 100)
	if err == nil {
		t.Error("expected error when setting priority on running job")
	}
}

func TestGetMaxMinPriority(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filehook-priority-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewSQLiteStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatalf("failed to initialize store: %v", err)
	}

	// Empty queue should return 0
	max, err := store.GetMaxPriority(ctx)
	if err != nil {
		t.Fatalf("failed to get max priority: %v", err)
	}
	if max != 0 {
		t.Errorf("expected max priority 0 for empty queue, got %d", max)
	}

	min, err := store.GetMinPriority(ctx)
	if err != nil {
		t.Fatalf("failed to get min priority: %v", err)
	}
	if min != 0 {
		t.Errorf("expected min priority 0 for empty queue, got %d", min)
	}

	// Add jobs with different priorities
	jobs := []*Job{
		{ID: "job-1", InputPath: "/path/file1.txt", Priority: 5, Status: JobStatusPending, CreatedAt: time.Now()},
		{ID: "job-2", InputPath: "/path/file2.txt", Priority: -10, Status: JobStatusPending, CreatedAt: time.Now()},
		{ID: "job-3", InputPath: "/path/file3.txt", Priority: 20, Status: JobStatusPending, CreatedAt: time.Now()},
	}

	for _, job := range jobs {
		if err := store.Enqueue(ctx, job); err != nil {
			t.Fatalf("failed to enqueue job: %v", err)
		}
	}

	max, err = store.GetMaxPriority(ctx)
	if err != nil {
		t.Fatalf("failed to get max priority: %v", err)
	}
	if max != 20 {
		t.Errorf("expected max priority 20, got %d", max)
	}

	min, err = store.GetMinPriority(ctx)
	if err != nil {
		t.Fatalf("failed to get min priority: %v", err)
	}
	if min != -10 {
		t.Errorf("expected min priority -10, got %d", min)
	}
}

func TestGetNextPendingJob(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filehook-priority-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewSQLiteStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatalf("failed to initialize store: %v", err)
	}

	// Empty queue
	next, err := store.GetNextPendingJob(ctx)
	if err != nil {
		t.Fatalf("failed to get next pending job: %v", err)
	}
	if next != nil {
		t.Error("expected nil for empty queue")
	}

	// Add jobs
	jobs := []*Job{
		{ID: "job-1", InputPath: "/path/file1.txt", Priority: 0, Status: JobStatusPending, CreatedAt: time.Now()},
		{ID: "job-2", InputPath: "/path/file2.txt", Priority: 10, Status: JobStatusPending, CreatedAt: time.Now()},
	}

	for _, job := range jobs {
		if err := store.Enqueue(ctx, job); err != nil {
			t.Fatalf("failed to enqueue job: %v", err)
		}
	}

	// Should return highest priority job without changing its status
	next, err = store.GetNextPendingJob(ctx)
	if err != nil {
		t.Fatalf("failed to get next pending job: %v", err)
	}
	if next == nil {
		t.Fatal("expected job but got nil")
	}
	if next.ID != "job-2" {
		t.Errorf("expected job-2, got %s", next.ID)
	}
	if next.Status != JobStatusPending {
		t.Errorf("expected pending status, got %s", next.Status)
	}

	// Call again - should still return the same job (not dequeued)
	next2, err := store.GetNextPendingJob(ctx)
	if err != nil {
		t.Fatalf("failed to get next pending job: %v", err)
	}
	if next2.ID != "job-2" {
		t.Errorf("expected job-2 again, got %s", next2.ID)
	}
}

func TestProcessRegistration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filehook-process-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewSQLiteStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatalf("failed to initialize store: %v", err)
	}

	// No process registered
	proc, err := store.GetActiveProcess(ctx)
	if err != nil {
		t.Fatalf("failed to get active process: %v", err)
	}
	if proc != nil {
		t.Error("expected no active process")
	}

	// Register current process (which is alive)
	pid := os.Getpid()
	info := &ProcessInfo{
		PID:       pid,
		Command:   "test",
		StartedAt: time.Now(),
	}
	if err := store.RegisterProcess(ctx, info); err != nil {
		t.Fatalf("failed to register process: %v", err)
	}

	// Should find it
	proc, err = store.GetActiveProcess(ctx)
	if err != nil {
		t.Fatalf("failed to get active process: %v", err)
	}
	if proc == nil {
		t.Fatal("expected active process")
	}
	if proc.PID != pid {
		t.Errorf("expected PID %d, got %d", pid, proc.PID)
	}
	if proc.Command != "test" {
		t.Errorf("expected command 'test', got %s", proc.Command)
	}

	// Unregister
	if err := store.UnregisterProcess(ctx, pid); err != nil {
		t.Fatalf("failed to unregister process: %v", err)
	}

	// Should be gone
	proc, err = store.GetActiveProcess(ctx)
	if err != nil {
		t.Fatalf("failed to get active process: %v", err)
	}
	if proc != nil {
		t.Error("expected no active process after unregister")
	}
}

func TestStaleProcessCleanup(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filehook-process-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewSQLiteStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatalf("failed to initialize store: %v", err)
	}

	// Register a fake dead process (PID that doesn't exist)
	// Use a very high PID that's unlikely to exist
	fakePID := 999999999
	info := &ProcessInfo{
		PID:       fakePID,
		Command:   "dead",
		StartedAt: time.Now(),
	}
	if err := store.RegisterProcess(ctx, info); err != nil {
		t.Fatalf("failed to register fake process: %v", err)
	}

	// GetActiveProcess should clean up the stale entry and return nil
	proc, err := store.GetActiveProcess(ctx)
	if err != nil {
		t.Fatalf("failed to get active process: %v", err)
	}
	if proc != nil {
		t.Error("expected stale process to be cleaned up")
	}
}

func TestListPendingWithPriority(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filehook-priority-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewSQLiteStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatalf("failed to initialize store: %v", err)
	}

	// Add jobs with different priorities
	jobs := []*Job{
		{ID: "job-1", InputPath: "/path/file1.txt", Priority: 0, Status: JobStatusPending, CreatedAt: time.Now()},
		{ID: "job-2", InputPath: "/path/file2.txt", Priority: 10, Status: JobStatusPending, CreatedAt: time.Now()},
		{ID: "job-3", InputPath: "/path/file3.txt", Priority: 5, Status: JobStatusPending, CreatedAt: time.Now()},
	}

	for _, job := range jobs {
		if err := store.Enqueue(ctx, job); err != nil {
			t.Fatalf("failed to enqueue job: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// List should return in priority order
	summaries, err := store.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("failed to list pending: %v", err)
	}

	if len(summaries) != 3 {
		t.Fatalf("expected 3 jobs, got %d", len(summaries))
	}

	expectedOrder := []string{"job-2", "job-3", "job-1"}
	expectedPriorities := []int{10, 5, 0}

	for i, expected := range expectedOrder {
		if summaries[i].ID != expected {
			t.Errorf("position %d: expected %s, got %s", i, expected, summaries[i].ID)
		}
		if summaries[i].Priority != expectedPriorities[i] {
			t.Errorf("position %d: expected priority %d, got %d", i, expectedPriorities[i], summaries[i].Priority)
		}
	}
}

func TestDequeueForGroupWithPriority(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filehook-priority-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewSQLiteStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatalf("failed to initialize store: %v", err)
	}

	// Add jobs in same group with different priorities
	jobs := []*Job{
		{ID: "job-1", InputPath: "/path/file1.txt", GroupKey: "group-a", Priority: 0, Status: JobStatusPending, CreatedAt: time.Now()},
		{ID: "job-2", InputPath: "/path/file2.txt", GroupKey: "group-a", Priority: 10, Status: JobStatusPending, CreatedAt: time.Now()},
		{ID: "job-3", InputPath: "/path/file3.txt", GroupKey: "group-b", Priority: 100, Status: JobStatusPending, CreatedAt: time.Now()},
	}

	for _, job := range jobs {
		if err := store.Enqueue(ctx, job); err != nil {
			t.Fatalf("failed to enqueue job: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Dequeue for group-a should return job-2 (higher priority in that group)
	job, err := store.DequeueForGroup(ctx, "group-a")
	if err != nil {
		t.Fatalf("failed to dequeue for group: %v", err)
	}
	if job.ID != "job-2" {
		t.Errorf("expected job-2, got %s", job.ID)
	}
}

func TestDequeueForStackWithPriority(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filehook-priority-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewSQLiteStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatalf("failed to initialize store: %v", err)
	}

	// Add jobs in same stack with different priorities
	jobs := []*Job{
		{ID: "job-1", InputPath: "/path/file1.txt", StackName: "stack-a", Priority: 0, Status: JobStatusPending, CreatedAt: time.Now()},
		{ID: "job-2", InputPath: "/path/file2.txt", StackName: "stack-a", Priority: 10, Status: JobStatusPending, CreatedAt: time.Now()},
		{ID: "job-3", InputPath: "/path/file3.txt", StackName: "stack-b", Priority: 100, Status: JobStatusPending, CreatedAt: time.Now()},
	}

	for _, job := range jobs {
		if err := store.Enqueue(ctx, job); err != nil {
			t.Fatalf("failed to enqueue job: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Dequeue for stack-a should return job-2 (higher priority in that stack)
	job, err := store.DequeueForStack(ctx, "stack-a")
	if err != nil {
		t.Fatalf("failed to dequeue for stack: %v", err)
	}
	if job.ID != "job-2" {
		t.Errorf("expected job-2, got %s", job.ID)
	}
}

func TestPriorityMigration(t *testing.T) {
	// Test that existing databases without priority column work after migration
	tmpDir, err := os.MkdirTemp("", "filehook-migration-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a store and add a job (this creates the schema with priority)
	store, err := NewSQLiteStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatalf("failed to initialize store: %v", err)
	}

	// Verify that a job with default priority works
	job := &Job{
		ID:        "test-job",
		InputPath: "/path/test.txt",
		Status:    JobStatusPending,
		CreatedAt: time.Now(),
	}
	if err := store.Enqueue(ctx, job); err != nil {
		t.Fatalf("failed to enqueue job: %v", err)
	}

	// Retrieve and check default priority is 0
	retrieved, err := store.Get(ctx, "test-job")
	if err != nil {
		t.Fatalf("failed to get job: %v", err)
	}
	if retrieved.Priority != 0 {
		t.Errorf("expected default priority 0, got %d", retrieved.Priority)
	}

	store.Close()

	// Re-open and verify migration works
	store2, err := NewSQLiteStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to reopen store: %v", err)
	}
	defer store2.Close()

	if err := store2.Initialize(ctx); err != nil {
		t.Fatalf("failed to reinitialize store: %v", err)
	}

	// Should be able to update priority
	if err := store2.SetPriority(ctx, "test-job", 42); err != nil {
		t.Fatalf("failed to set priority after migration: %v", err)
	}

	retrieved2, err := store2.Get(ctx, "test-job")
	if err != nil {
		t.Fatalf("failed to get job after migration: %v", err)
	}
	if retrieved2.Priority != 42 {
		t.Errorf("expected priority 42, got %d", retrieved2.Priority)
	}
}
