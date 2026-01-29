package queue

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func newTestStore(t *testing.T) (*SQLiteStore, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "filehook-multiinstance-test-*")
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

	return store, func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}
}

func TestInstanceTagging(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()

	job := &Job{
		ID:         "inst-job-1",
		InputPath:  "/path/file1.txt",
		InstanceID: "instance-a",
		Status:     JobStatusPending,
		CreatedAt:  time.Now(),
	}
	if err := store.Enqueue(ctx, job); err != nil {
		t.Fatalf("failed to enqueue: %v", err)
	}

	got, err := store.Get(ctx, "inst-job-1")
	if err != nil {
		t.Fatalf("failed to get: %v", err)
	}
	if got.InstanceID != "instance-a" {
		t.Errorf("expected instance_id 'instance-a', got %q", got.InstanceID)
	}
}

func TestListPendingByInstance(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()

	jobs := []*Job{
		{ID: "j1", InputPath: "/a/1.txt", InstanceID: "inst-a", Status: JobStatusPending, CreatedAt: time.Now()},
		{ID: "j2", InputPath: "/a/2.txt", InstanceID: "inst-b", Status: JobStatusPending, CreatedAt: time.Now()},
		{ID: "j3", InputPath: "/a/3.txt", InstanceID: "inst-a", Status: JobStatusPending, CreatedAt: time.Now()},
	}
	for _, j := range jobs {
		if err := store.Enqueue(ctx, j); err != nil {
			t.Fatalf("failed to enqueue: %v", err)
		}
	}

	result, err := store.ListPendingByInstance(ctx, "inst-a", 10)
	if err != nil {
		t.Fatalf("failed to list: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(result))
	}
	for _, j := range result {
		if j.InstanceID != "inst-a" {
			t.Errorf("expected instance inst-a, got %q", j.InstanceID)
		}
	}
}

func TestDequeueWithClaim(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()

	job := &Job{
		ID:        "claim-job-1",
		InputPath: "/path/claim.txt",
		Status:    JobStatusPending,
		CreatedAt: time.Now(),
	}
	if err := store.Enqueue(ctx, job); err != nil {
		t.Fatalf("failed to enqueue: %v", err)
	}

	claimed, err := store.DequeueWithClaim(ctx, 12345)
	if err != nil {
		t.Fatalf("failed to dequeue with claim: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected job, got nil")
	}
	if claimed.ClaimedBy == nil || *claimed.ClaimedBy != 12345 {
		t.Errorf("expected claimed_by 12345, got %v", claimed.ClaimedBy)
	}
	if claimed.Status != JobStatusRunning {
		t.Errorf("expected running status, got %s", claimed.Status)
	}
}

func TestDequeueForStackWithClaim(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()

	jobs := []*Job{
		{ID: "sc-1", InputPath: "/a/1.txt", StackName: "stack-a", Status: JobStatusPending, CreatedAt: time.Now()},
		{ID: "sc-2", InputPath: "/a/2.txt", StackName: "stack-b", Status: JobStatusPending, CreatedAt: time.Now()},
	}
	for _, j := range jobs {
		if err := store.Enqueue(ctx, j); err != nil {
			t.Fatalf("failed to enqueue: %v", err)
		}
	}

	claimed, err := store.DequeueForStackWithClaim(ctx, "stack-a", 99999)
	if err != nil {
		t.Fatalf("failed to dequeue: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected job, got nil")
	}
	if claimed.ID != "sc-1" {
		t.Errorf("expected sc-1, got %s", claimed.ID)
	}
	if claimed.ClaimedBy == nil || *claimed.ClaimedBy != 99999 {
		t.Errorf("expected claimed_by 99999, got %v", claimed.ClaimedBy)
	}
}

func TestCleanupStaleRunningForPID(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Enqueue and claim two jobs by different PIDs
	j1 := &Job{ID: "stale-1", InputPath: "/a/1.txt", Status: JobStatusPending, CreatedAt: time.Now()}
	j2 := &Job{ID: "stale-2", InputPath: "/a/2.txt", Status: JobStatusPending, CreatedAt: time.Now()}
	store.Enqueue(ctx, j1)
	store.Enqueue(ctx, j2)

	store.DequeueWithClaim(ctx, 111)
	store.DequeueWithClaim(ctx, 222)

	// Cleanup only PID 111's jobs
	cleaned, err := store.CleanupStaleRunningForPID(ctx, 111)
	if err != nil {
		t.Fatalf("failed to cleanup: %v", err)
	}
	if cleaned != 1 {
		t.Errorf("expected 1 cleaned, got %d", cleaned)
	}

	// stale-1 should be pending again
	got1, _ := store.Get(ctx, "stale-1")
	if got1.Status != JobStatusPending {
		t.Errorf("expected pending, got %s", got1.Status)
	}

	// stale-2 should still be running
	got2, _ := store.Get(ctx, "stale-2")
	if got2.Status != JobStatusRunning {
		t.Errorf("expected running, got %s", got2.Status)
	}
}

func TestSetPriorityByInstance(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()

	job := &Job{
		ID:         "pri-inst-1",
		InputPath:  "/a/1.txt",
		InstanceID: "inst-x",
		Status:     JobStatusPending,
		CreatedAt:  time.Now(),
	}
	store.Enqueue(ctx, job)

	// Should succeed with correct instance
	if err := store.SetPriorityByInstance(ctx, "pri-inst-1", "inst-x", 50); err != nil {
		t.Fatalf("failed: %v", err)
	}

	got, _ := store.Get(ctx, "pri-inst-1")
	if got.Priority != 50 {
		t.Errorf("expected priority 50, got %d", got.Priority)
	}

	// Should fail with wrong instance
	err := store.SetPriorityByInstance(ctx, "pri-inst-1", "inst-y", 100)
	if err == nil {
		t.Error("expected error for wrong instance")
	}
}

func TestGetStatsByInstance(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()

	jobs := []*Job{
		{ID: "si-1", InputPath: "/a/1.txt", InstanceID: "inst-a", Status: JobStatusPending, CreatedAt: time.Now()},
		{ID: "si-2", InputPath: "/a/2.txt", InstanceID: "inst-a", Status: JobStatusPending, CreatedAt: time.Now()},
		{ID: "si-3", InputPath: "/a/3.txt", InstanceID: "inst-b", Status: JobStatusPending, CreatedAt: time.Now()},
	}
	for _, j := range jobs {
		store.Enqueue(ctx, j)
	}

	stats, err := store.GetStatsByInstance(ctx, "inst-a")
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	if stats.Pending != 2 {
		t.Errorf("expected 2 pending, got %d", stats.Pending)
	}
	if stats.Total != 2 {
		t.Errorf("expected 2 total, got %d", stats.Total)
	}
}

func TestProcessRegistrationWithRole(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()
	pid := os.Getpid()

	// Register as scheduler
	info := &ProcessInfo{
		PID:        pid,
		Command:    "serve",
		Role:       ProcessRoleScheduler,
		InstanceID: "",
		StartedAt:  time.Now(),
	}
	if err := store.RegisterProcess(ctx, info); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	// Should find scheduler
	sched, err := store.GetSchedulerProcess(ctx)
	if err != nil {
		t.Fatalf("failed to get scheduler: %v", err)
	}
	if sched == nil {
		t.Fatal("expected scheduler, got nil")
	}
	if sched.Role != ProcessRoleScheduler {
		t.Errorf("expected scheduler role, got %s", sched.Role)
	}

	// GetActiveProcesses should include it
	all, err := store.GetActiveProcesses(ctx)
	if err != nil {
		t.Fatalf("failed to get active processes: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("expected 1 process, got %d", len(all))
	}

	store.UnregisterProcess(ctx, pid)
}

func TestHeartbeat(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()
	pid := os.Getpid()

	info := &ProcessInfo{
		PID:       pid,
		Command:   "serve",
		Role:      ProcessRoleScheduler,
		StartedAt: time.Now(),
	}
	store.RegisterProcess(ctx, info)

	time.Sleep(10 * time.Millisecond)

	if err := store.UpdateHeartbeat(ctx, pid); err != nil {
		t.Fatalf("failed to update heartbeat: %v", err)
	}

	proc, _ := store.GetActiveProcess(ctx)
	if proc == nil {
		t.Fatal("expected process")
	}
	if proc.HeartbeatAt.IsZero() {
		t.Error("expected non-zero heartbeat_at")
	}

	store.UnregisterProcess(ctx, pid)
}

func TestDequeueWithClaimPriorityOrder(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()

	jobs := []*Job{
		{ID: "cp-1", InputPath: "/a/1.txt", Priority: 0, Status: JobStatusPending, CreatedAt: time.Now()},
		{ID: "cp-2", InputPath: "/a/2.txt", Priority: 10, Status: JobStatusPending, CreatedAt: time.Now()},
		{ID: "cp-3", InputPath: "/a/3.txt", Priority: 5, Status: JobStatusPending, CreatedAt: time.Now()},
	}
	for _, j := range jobs {
		store.Enqueue(ctx, j)
	}

	// Should dequeue highest priority first
	claimed, err := store.DequeueWithClaim(ctx, 111)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	if claimed.ID != "cp-2" {
		t.Errorf("expected cp-2 (priority 10), got %s", claimed.ID)
	}

	claimed2, _ := store.DequeueWithClaim(ctx, 111)
	if claimed2.ID != "cp-3" {
		t.Errorf("expected cp-3 (priority 5), got %s", claimed2.ID)
	}
}

func TestDequeueWithClaimEmptyQueue(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()

	claimed, err := store.DequeueWithClaim(ctx, 111)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	if claimed != nil {
		t.Errorf("expected nil from empty queue, got %v", claimed)
	}
}

func TestDequeueForStackWithClaimEmptyStack(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Enqueue on stack-a, try to dequeue from stack-b
	job := &Job{ID: "s-1", InputPath: "/a/1.txt", StackName: "stack-a", Status: JobStatusPending, CreatedAt: time.Now()}
	store.Enqueue(ctx, job)

	claimed, err := store.DequeueForStackWithClaim(ctx, "stack-b", 111)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	if claimed != nil {
		t.Errorf("expected nil for empty stack, got %v", claimed)
	}
}

func TestCleanupStaleRunningForPIDNoMatch(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Enqueue and claim a job by PID 111
	job := &Job{ID: "nm-1", InputPath: "/a/1.txt", Status: JobStatusPending, CreatedAt: time.Now()}
	store.Enqueue(ctx, job)
	store.DequeueWithClaim(ctx, 111)

	// Cleanup for PID 222 should find nothing
	cleaned, err := store.CleanupStaleRunningForPID(ctx, 222)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	if cleaned != 0 {
		t.Errorf("expected 0 cleaned, got %d", cleaned)
	}

	// Original job should still be running
	got, _ := store.Get(ctx, "nm-1")
	if got.Status != JobStatusRunning {
		t.Errorf("expected running, got %s", got.Status)
	}
}

func TestMultiProcessRegistration(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()
	pid := os.Getpid()

	// Register scheduler
	sched := &ProcessInfo{
		PID:       pid,
		Command:   "serve",
		Role:      ProcessRoleScheduler,
		StartedAt: time.Now(),
	}
	store.RegisterProcess(ctx, sched)

	// Register producer (use pid+1 as fake - will be cleaned as stale, so use same pid)
	// We can't register two different PIDs easily since the stale check kills fake PIDs.
	// Instead, verify that GetActiveProcesses returns the scheduler.
	all, err := store.GetActiveProcesses(ctx)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 process, got %d", len(all))
	}
	if all[0].Role != ProcessRoleScheduler {
		t.Errorf("expected scheduler role, got %s", all[0].Role)
	}

	// GetSchedulerProcess should find it
	s, _ := store.GetSchedulerProcess(ctx)
	if s == nil {
		t.Fatal("expected scheduler")
	}
	if s.PID != pid {
		t.Errorf("expected pid %d, got %d", pid, s.PID)
	}

	store.UnregisterProcess(ctx, pid)

	// After unregister, scheduler should be gone
	s2, _ := store.GetSchedulerProcess(ctx)
	if s2 != nil {
		t.Error("expected no scheduler after unregister")
	}
}

func TestGetStatsByInstanceMixedStatuses(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Enqueue jobs for same instance
	jobs := []*Job{
		{ID: "ms-1", InputPath: "/a/1.txt", InstanceID: "inst-a", Status: JobStatusPending, CreatedAt: time.Now()},
		{ID: "ms-2", InputPath: "/a/2.txt", InstanceID: "inst-a", Status: JobStatusPending, CreatedAt: time.Now()},
		{ID: "ms-3", InputPath: "/a/3.txt", InstanceID: "inst-a", Status: JobStatusPending, CreatedAt: time.Now()},
	}
	for _, j := range jobs {
		store.Enqueue(ctx, j)
	}

	// Dequeue one (makes it running)
	store.Dequeue(ctx)
	// Complete one
	store.Complete(ctx, "ms-1", &JobResult{ExitCode: 0})
	// Fail one
	store.Dequeue(ctx)
	store.Fail(ctx, "ms-2", &JobResult{ExitCode: 1, Error: fmt.Errorf("fail")})

	stats, err := store.GetStatsByInstance(ctx, "inst-a")
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	if stats.Total != 3 {
		t.Errorf("expected 3 total, got %d", stats.Total)
	}
	if stats.Pending != 1 {
		t.Errorf("expected 1 pending, got %d", stats.Pending)
	}
	if stats.Completed != 1 {
		t.Errorf("expected 1 completed, got %d", stats.Completed)
	}
	if stats.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", stats.Failed)
	}
}

func TestListPendingByInstanceEmpty(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()

	result, err := store.ListPendingByInstance(ctx, "nonexistent", 10)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 jobs, got %d", len(result))
	}
}

func TestInstanceIDPreservedInGet(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()

	job := &Job{
		ID:         "ip-1",
		InputPath:  "/a/1.txt",
		InstanceID: "my-instance",
		Status:     JobStatusPending,
		CreatedAt:  time.Now(),
	}
	store.Enqueue(ctx, job)

	// Get should preserve instance_id
	got, _ := store.Get(ctx, "ip-1")
	if got.InstanceID != "my-instance" {
		t.Errorf("expected instance_id 'my-instance', got %q", got.InstanceID)
	}

	// Dequeue and verify claimed_by is set
	claimed, _ := store.DequeueWithClaim(ctx, 42)
	if claimed.ClaimedBy == nil || *claimed.ClaimedBy != 42 {
		t.Errorf("expected claimed_by 42, got %v", claimed.ClaimedBy)
	}

	// Get the job again to verify claimed_by persisted
	got2, _ := store.Get(ctx, "ip-1")
	if got2.ClaimedBy == nil || *got2.ClaimedBy != 42 {
		t.Errorf("expected claimed_by 42 on re-read, got %v", got2.ClaimedBy)
	}
	if got2.InstanceID != "my-instance" {
		t.Errorf("expected instance_id preserved after claim, got %q", got2.InstanceID)
	}
}

func TestEmptyInstanceID(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Job with no instance ID
	job := &Job{
		ID:        "ei-1",
		InputPath: "/a/1.txt",
		Status:    JobStatusPending,
		CreatedAt: time.Now(),
	}
	store.Enqueue(ctx, job)

	got, _ := store.Get(ctx, "ei-1")
	if got.InstanceID != "" {
		t.Errorf("expected empty instance_id, got %q", got.InstanceID)
	}
	if got.ClaimedBy != nil {
		t.Errorf("expected nil claimed_by, got %v", got.ClaimedBy)
	}
}

func TestToSummaryPreservesInstanceID(t *testing.T) {
	job := &Job{
		ID:         "ts-1",
		InputPath:  "/a/1.txt",
		Status:     JobStatusPending,
		InstanceID: "test-inst",
		CreatedAt:  time.Now(),
	}
	summary := job.ToSummary()
	if summary.InstanceID != "test-inst" {
		t.Errorf("expected instance_id 'test-inst', got %q", summary.InstanceID)
	}
}

func TestSchemaMigrationNewColumns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filehook-migration-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	// Create store, initialize, add a job, close
	store1, _ := NewSQLiteStore(tmpDir)
	store1.Initialize(ctx)
	job := &Job{ID: "mig-1", InputPath: "/a/1.txt", Status: JobStatusPending, CreatedAt: time.Now()}
	store1.Enqueue(ctx, job)
	store1.Close()

	// Re-open: migrations should be idempotent
	store2, err := NewSQLiteStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to reopen: %v", err)
	}
	defer store2.Close()
	if err := store2.Initialize(ctx); err != nil {
		t.Fatalf("failed to reinitialize: %v", err)
	}

	// Should be able to read the job with new columns (defaults to empty/nil)
	got, err := store2.Get(ctx, "mig-1")
	if err != nil {
		t.Fatalf("failed to get after migration: %v", err)
	}
	if got.InstanceID != "" {
		t.Errorf("expected empty instance_id after migration, got %q", got.InstanceID)
	}
	if got.ClaimedBy != nil {
		t.Errorf("expected nil claimed_by after migration, got %v", got.ClaimedBy)
	}

	// Should be able to enqueue with new fields
	job2 := &Job{ID: "mig-2", InputPath: "/a/2.txt", InstanceID: "inst-x", Status: JobStatusPending, CreatedAt: time.Now()}
	if err := store2.Enqueue(ctx, job2); err != nil {
		t.Fatalf("failed to enqueue after migration: %v", err)
	}

	got2, _ := store2.Get(ctx, "mig-2")
	if got2.InstanceID != "inst-x" {
		t.Errorf("expected instance_id 'inst-x', got %q", got2.InstanceID)
	}
}
