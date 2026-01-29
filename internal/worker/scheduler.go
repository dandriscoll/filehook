package worker

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/dandriscoll/filehook/internal/config"
	"github.com/dandriscoll/filehook/internal/debug"
	"github.com/dandriscoll/filehook/internal/output"
	"github.com/dandriscoll/filehook/internal/plugin"
	"github.com/dandriscoll/filehook/internal/queue"
)

// CentralScheduler is the central job scheduler for multi-instance mode.
// It owns the resource, manages stack switching, and executes jobs.
type CentralScheduler struct {
	cfg          *config.Config
	store        queue.Store
	executor     *Executor
	pid          int
	currentStack string
	stopCh       chan struct{}
	wg           sync.WaitGroup
	mu           sync.Mutex
	logger       *output.Logger
	debugLogger  *debug.Logger
}

// NewCentralScheduler creates a new central scheduler
func NewCentralScheduler(cfg *config.Config, store queue.Store, namingPlugin *plugin.NamingPlugin, debugLogger *debug.Logger, logger *output.Logger, pid int) (*CentralScheduler, error) {
	executor, err := NewExecutor(cfg, namingPlugin, debugLogger)
	if err != nil {
		return nil, err
	}

	return &CentralScheduler{
		cfg:         cfg,
		store:       store,
		executor:    executor,
		pid:         pid,
		stopCh:      make(chan struct{}),
		logger:      logger,
		debugLogger: debugLogger,
	}, nil
}

// Start begins the scheduler loop and heartbeat goroutine
func (cs *CentralScheduler) Start(ctx context.Context) {
	cs.wg.Add(2)
	go cs.run(ctx)
	go cs.heartbeat(ctx)
}

// Stop stops the scheduler and waits for it to finish
func (cs *CentralScheduler) Stop() {
	close(cs.stopCh)
	cs.wg.Wait()
}

func (cs *CentralScheduler) heartbeat(ctx context.Context) {
	defer cs.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-cs.stopCh:
			return
		case <-ticker.C:
			if err := cs.store.UpdateHeartbeat(ctx, cs.pid); err != nil {
				cs.debugLogger.Log("heartbeat update failed: %v", err)
			}
		}
	}
}

func (cs *CentralScheduler) run(ctx context.Context) {
	defer cs.wg.Done()

	isStackMode := cs.cfg.Concurrency.Mode == config.ConcurrencyStack

	// Restore current stack from database if in stack mode
	if isStackMode {
		state, err := cs.store.GetStackState(ctx)
		if err != nil {
			cs.logger.Error("failed to get stack state: %v", err)
		} else if state.CurrentStack != "" {
			cs.mu.Lock()
			cs.currentStack = state.CurrentStack
			cs.mu.Unlock()
			cs.logger.Info("restored stack: %s", state.CurrentStack)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-cs.stopCh:
			return
		default:
		}

		var job *queue.Job
		var err error

		if isStackMode {
			job, err = cs.processStackMode(ctx)
		} else {
			job, err = cs.store.DequeueWithClaim(ctx, cs.pid)
		}

		if err != nil {
			cs.logger.Error("dequeue error: %v", err)
			time.Sleep(time.Second)
			continue
		}

		if job == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		cs.executeJob(ctx, job)
	}
}

func (cs *CentralScheduler) processStackMode(ctx context.Context) (*queue.Job, error) {
	counts, err := cs.store.GetPendingCountByStack(ctx)
	if err != nil {
		return nil, err
	}

	if len(counts) == 0 {
		return nil, nil
	}

	cs.mu.Lock()
	currentStack := cs.currentStack
	cs.mu.Unlock()

	nextStack := cs.selectStack(currentStack, counts)
	if nextStack == "" {
		return nil, nil
	}

	if currentStack != nextStack {
		if err := cs.switchToStack(ctx, nextStack); err != nil {
			return nil, fmt.Errorf("failed to switch to stack %s: %w", nextStack, err)
		}
	}

	return cs.store.DequeueForStackWithClaim(ctx, nextStack, cs.pid)
}

func (cs *CentralScheduler) selectStack(currentStack string, counts map[string]int) string {
	if currentStack != "" {
		if count, ok := counts[currentStack]; ok && count > 0 {
			return currentStack
		}
	}

	var maxStack string
	var maxCount int
	for stack, count := range counts {
		if count > maxCount {
			maxCount = count
			maxStack = stack
		}
	}
	return maxStack
}

func (cs *CentralScheduler) switchToStack(ctx context.Context, stackName string) error {
	stackDef := cs.cfg.GetStackDefinition(stackName)
	if stackDef == nil {
		return fmt.Errorf("stack %q not defined", stackName)
	}

	switchCmd := stackDef.GetSwitchCommand()
	if len(switchCmd) == 0 {
		return fmt.Errorf("stack %q has no switch command defined", stackName)
	}

	// Resolve the first element (the executable) relative to config dir
	resolved := make([]string, len(switchCmd))
	copy(resolved, switchCmd)
	resolved[0] = cs.cfg.ResolvePath(resolved[0])

	cs.logger.StackSwitching(stackName)

	start := time.Now()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, resolved[0], resolved[1:]...)
	cmd.Dir = cs.cfg.ConfigDir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	duration := time.Since(start)

	if err != nil {
		return fmt.Errorf("switch script failed: %w (stderr: %s)", err, stderr.String())
	}

	if storeErr := cs.store.SetCurrentStack(ctx, stackName, duration.Milliseconds()); storeErr != nil {
		cs.logger.Error("failed to update stack state: %v", storeErr)
	}

	cs.mu.Lock()
	cs.currentStack = stackName
	cs.mu.Unlock()

	cs.logger.StackSwitch(stackName, duration.Milliseconds())
	return nil
}

func (cs *CentralScheduler) executeJob(ctx context.Context, job *queue.Job) {
	cs.logger.Processing(job.InputPath)

	result := cs.executor.Execute(ctx, job)

	if result.Error != nil || result.ExitCode != 0 {
		if err := cs.store.Fail(ctx, job.ID, result); err != nil {
			cs.logger.Error("failed to mark job failed: %v", err)
		}
		cs.logger.Failed(job.InputPath, result.ExitCode)
	} else {
		if err := cs.store.Complete(ctx, job.ID, result); err != nil {
			cs.logger.Error("failed to mark job complete: %v", err)
		}
		cs.logger.Completed(job.InputPath, result.DurationMs)
	}

	if job.StackName != "" {
		if err := cs.store.UpdateStackJobStats(ctx, job.StackName, result.DurationMs); err != nil {
			cs.debugLogger.Log("Failed to update stack stats: %v", err)
		}
	}
}
