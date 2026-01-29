package worker

import (
	"context"
	"sync"
	"time"

	"github.com/dandriscoll/filehook/internal/config"
	"github.com/dandriscoll/filehook/internal/debug"
	"github.com/dandriscoll/filehook/internal/output"
	"github.com/dandriscoll/filehook/internal/plugin"
	"github.com/dandriscoll/filehook/internal/queue"
)

// SequentialScheduler processes jobs one at a time, switching between groups
type SequentialScheduler struct {
	cfg      *config.Config
	store    queue.Store
	executor *Executor
	wg       sync.WaitGroup
	stopCh   chan struct{}
	logger   *output.Logger

	// Track current group and round-robin state
	mu           sync.Mutex
	currentGroup string
	groupIndex   int
}

// NewSequentialScheduler creates a new sequential scheduler
func NewSequentialScheduler(cfg *config.Config, store queue.Store, namingPlugin *plugin.NamingPlugin, debugLogger *debug.Logger, logger *output.Logger) (*SequentialScheduler, error) {
	executor, err := NewExecutor(cfg, namingPlugin, debugLogger)
	if err != nil {
		return nil, err
	}

	return &SequentialScheduler{
		cfg:      cfg,
		store:    store,
		executor: executor,
		stopCh:   make(chan struct{}),
		logger:   logger,
	}, nil
}

// Start begins the scheduler
func (s *SequentialScheduler) Start(ctx context.Context) {
	s.wg.Add(1)
	go s.run(ctx)
}

// Stop stops the scheduler and waits for it to finish
func (s *SequentialScheduler) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}

func (s *SequentialScheduler) run(ctx context.Context) {
	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		default:
		}

		// Get available groups
		groups, err := s.store.GetDistinctGroups(ctx)
		if err != nil {
			s.logger.Error("failed to get groups: %v", err)
			time.Sleep(time.Second)
			continue
		}

		if len(groups) == 0 {
			// No pending jobs, check for jobs without group
			job, err := s.store.Dequeue(ctx)
			if err != nil {
				s.logger.Error("failed to dequeue: %v", err)
				time.Sleep(time.Second)
				continue
			}

			if job == nil {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			s.processJob(ctx, job)
			continue
		}

		// Round-robin through groups
		s.mu.Lock()
		if s.groupIndex >= len(groups) {
			s.groupIndex = 0
		}
		groupKey := groups[s.groupIndex]
		s.groupIndex++
		s.currentGroup = groupKey
		s.mu.Unlock()

		// Get next job from this group
		job, err := s.store.DequeueForGroup(ctx, groupKey)
		if err != nil {
			s.logger.Error("failed to dequeue for group %s: %v", groupKey, err)
			time.Sleep(time.Second)
			continue
		}

		if job == nil {
			continue
		}

		s.processJob(ctx, job)
	}
}

func (s *SequentialScheduler) processJob(ctx context.Context, job *queue.Job) {
	s.logger.Processing(job.InputPath)

	result := s.executor.Execute(ctx, job)

	if result.Error != nil || result.ExitCode != 0 {
		if err := s.store.Fail(ctx, job.ID, result); err != nil {
			s.logger.Error("failed to mark job failed: %v", err)
		}
		s.logger.Failed(job.InputPath, result.ExitCode)
	} else {
		if err := s.store.Complete(ctx, job.ID, result); err != nil {
			s.logger.Error("failed to mark job complete: %v", err)
		}
		s.logger.Completed(job.InputPath, result.DurationMs)
	}
}

// RunOnce processes all pending jobs sequentially and returns
func (s *SequentialScheduler) RunOnce(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		job, err := s.store.Dequeue(ctx)
		if err != nil {
			return err
		}

		if job == nil {
			return nil
		}

		s.processJob(ctx, job)
	}
}
