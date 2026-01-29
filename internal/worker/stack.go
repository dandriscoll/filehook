package worker

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os/exec"
	"sync"
	"time"

	"github.com/dandriscoll/filehook/internal/config"
	"github.com/dandriscoll/filehook/internal/debug"
	"github.com/dandriscoll/filehook/internal/plugin"
	"github.com/dandriscoll/filehook/internal/queue"
)

// StackScheduler processes jobs using mutually-exclusive stacks
// Only one stack can be active at a time, and switching between stacks
// requires running a switch script (which may take several minutes)
type StackScheduler struct {
	cfg          *config.Config
	store        queue.Store
	executor     *Executor
	currentStack string
	stopCh       chan struct{}
	wg           sync.WaitGroup
	mu           sync.Mutex
	logger       *log.Logger
	debugLogger  *debug.Logger
}

// NewStackScheduler creates a new stack scheduler
func NewStackScheduler(cfg *config.Config, store queue.Store, namingPlugin *plugin.NamingPlugin, debugLogger *debug.Logger, logger *log.Logger) (*StackScheduler, error) {
	executor, err := NewExecutor(cfg, namingPlugin, debugLogger)
	if err != nil {
		return nil, err
	}

	return &StackScheduler{
		cfg:         cfg,
		store:       store,
		executor:    executor,
		stopCh:      make(chan struct{}),
		logger:      logger,
		debugLogger: debugLogger,
	}, nil
}

// Start begins the scheduler in a goroutine
func (s *StackScheduler) Start(ctx context.Context) {
	s.wg.Add(1)
	go s.run(ctx)
}

// Stop stops the scheduler and waits for it to finish
func (s *StackScheduler) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}

func (s *StackScheduler) run(ctx context.Context) {
	defer s.wg.Done()

	// Restore current stack from database
	state, err := s.store.GetStackState(ctx)
	if err != nil {
		s.logger.Printf("stack-scheduler: failed to get stack state: %v", err)
	} else if state.CurrentStack != "" {
		s.mu.Lock()
		s.currentStack = state.CurrentStack
		s.mu.Unlock()
		s.logger.Printf("stack-scheduler: restored current stack: %s", state.CurrentStack)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		default:
		}

		// Get pending counts by stack
		counts, err := s.store.GetPendingCountByStack(ctx)
		if err != nil {
			s.logger.Printf("stack-scheduler: failed to get pending counts: %v", err)
			time.Sleep(time.Second)
			continue
		}

		if len(counts) == 0 {
			// No pending jobs
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Select next stack to process
		nextStack, err := s.selectNextStack(ctx, counts)
		if err != nil {
			s.logger.Printf("stack-scheduler: failed to select next stack: %v", err)
			time.Sleep(time.Second)
			continue
		}

		if nextStack == "" {
			// No stack with pending jobs
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Switch stacks if necessary
		s.mu.Lock()
		currentStack := s.currentStack
		s.mu.Unlock()

		if currentStack != nextStack {
			if err := s.switchToStack(ctx, nextStack); err != nil {
				s.logger.Printf("stack-scheduler: failed to switch to stack %s: %v", nextStack, err)
				time.Sleep(time.Second)
				continue
			}
		}

		// Process one job from the current stack
		job, err := s.store.DequeueForStack(ctx, nextStack)
		if err != nil {
			s.logger.Printf("stack-scheduler: failed to dequeue for stack %s: %v", nextStack, err)
			time.Sleep(time.Second)
			continue
		}

		if job == nil {
			// Stack is empty, will select new stack on next iteration
			continue
		}

		s.processJob(ctx, job)
	}
}

// selectNextStack determines which stack to process next
// Strategy: drain current stack first, then switch to stack with most pending jobs
func (s *StackScheduler) selectNextStack(ctx context.Context, counts map[string]int) (string, error) {
	s.mu.Lock()
	currentStack := s.currentStack
	s.mu.Unlock()

	// If current stack has pending jobs, continue with it
	if currentStack != "" {
		if count, ok := counts[currentStack]; ok && count > 0 {
			return currentStack, nil
		}
	}

	// Find stack with most pending jobs (largest batch first)
	var maxStack string
	var maxCount int

	for stack, count := range counts {
		if count > maxCount {
			maxCount = count
			maxStack = stack
		}
	}

	return maxStack, nil
}

// switchToStack executes the switch script for the target stack
func (s *StackScheduler) switchToStack(ctx context.Context, stackName string) error {
	stackDef := s.cfg.GetStackDefinition(stackName)
	if stackDef == nil {
		return fmt.Errorf("stack %q not defined", stackName)
	}

	switchCmd := stackDef.GetSwitchCommand()
	if len(switchCmd) == 0 {
		return fmt.Errorf("stack %q has no switch command defined", stackName)
	}

	resolved := make([]string, len(switchCmd))
	copy(resolved, switchCmd)
	resolved[0] = s.cfg.ResolvePath(resolved[0])

	s.logger.Printf("stack-scheduler: switching to stack %s (running %v)", stackName, resolved)
	s.debugLogger.Log("Stack switch: %s -> %s (cmd: %v)", s.currentStack, stackName, resolved)

	start := time.Now()

	// Execute the switch command
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, resolved[0], resolved[1:]...)
	cmd.Dir = s.cfg.ConfigDir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	duration := time.Since(start)
	durationMs := duration.Milliseconds()

	if err != nil {
		s.debugLogger.Log("Stack switch failed: %v, stderr: %s", err, stderr.String())
		return fmt.Errorf("switch script failed: %w (stderr: %s)", err, stderr.String())
	}

	// Update state in database
	if err := s.store.SetCurrentStack(ctx, stackName, durationMs); err != nil {
		s.logger.Printf("stack-scheduler: failed to update stack state: %v", err)
	}

	// Update in-memory state
	s.mu.Lock()
	s.currentStack = stackName
	s.mu.Unlock()

	s.logger.Printf("stack-scheduler: switched to stack %s in %v", stackName, duration)
	s.debugLogger.Log("Stack switch complete: now on %s (took %v)", stackName, duration)

	return nil
}

func (s *StackScheduler) processJob(ctx context.Context, job *queue.Job) {
	s.logger.Printf("stack-scheduler: processing %s (stack=%s)", job.InputPath, job.StackName)

	result := s.executor.Execute(ctx, job)

	if result.Error != nil || result.ExitCode != 0 {
		if err := s.store.Fail(ctx, job.ID, result); err != nil {
			s.logger.Printf("stack-scheduler: failed to mark job failed: %v", err)
		}
		s.logger.Printf("stack-scheduler: job failed: %s (exit=%d)", job.InputPath, result.ExitCode)
	} else {
		if err := s.store.Complete(ctx, job.ID, result); err != nil {
			s.logger.Printf("stack-scheduler: failed to mark job complete: %v", err)
		}
		s.logger.Printf("stack-scheduler: completed %s in %dms", job.InputPath, result.DurationMs)
	}

	// Update stack statistics
	if job.StackName != "" {
		if err := s.store.UpdateStackJobStats(ctx, job.StackName, result.DurationMs); err != nil {
			s.debugLogger.Log("Failed to update stack stats: %v", err)
		}
	}
}

// RunOnce processes all pending jobs and returns (for run mode)
func (s *StackScheduler) RunOnce(ctx context.Context) error {
	// Restore current stack from database
	state, err := s.store.GetStackState(ctx)
	if err != nil {
		s.logger.Printf("stack-scheduler: failed to get stack state: %v", err)
	} else if state.CurrentStack != "" {
		s.mu.Lock()
		s.currentStack = state.CurrentStack
		s.mu.Unlock()
		s.logger.Printf("stack-scheduler: restored current stack: %s", state.CurrentStack)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Get pending counts by stack
		counts, err := s.store.GetPendingCountByStack(ctx)
		if err != nil {
			return fmt.Errorf("failed to get pending counts: %w", err)
		}

		if len(counts) == 0 {
			// All done
			return nil
		}

		// Select next stack
		nextStack, err := s.selectNextStack(ctx, counts)
		if err != nil {
			return fmt.Errorf("failed to select next stack: %w", err)
		}

		if nextStack == "" {
			return nil
		}

		// Switch if necessary
		s.mu.Lock()
		currentStack := s.currentStack
		s.mu.Unlock()

		if currentStack != nextStack {
			if err := s.switchToStack(ctx, nextStack); err != nil {
				return fmt.Errorf("failed to switch to stack %s: %w", nextStack, err)
			}
		}

		// Drain all jobs from this stack before switching
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			job, err := s.store.DequeueForStack(ctx, nextStack)
			if err != nil {
				return fmt.Errorf("failed to dequeue: %w", err)
			}

			if job == nil {
				// Stack is empty, break to select next stack
				break
			}

			s.processJob(ctx, job)
		}
	}
}

// CurrentStack returns the current active stack (for status display)
func (s *StackScheduler) CurrentStack() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentStack
}
