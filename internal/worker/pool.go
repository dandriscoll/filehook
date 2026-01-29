package worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dandriscoll/filehook/internal/config"
	"github.com/dandriscoll/filehook/internal/debug"
	"github.com/dandriscoll/filehook/internal/output"
	"github.com/dandriscoll/filehook/internal/plugin"
	"github.com/dandriscoll/filehook/internal/queue"
)

// Pool manages parallel workers
type Pool struct {
	cfg      *config.Config
	store    queue.Store
	executor *Executor
	workers  int
	wg       sync.WaitGroup
	stopCh   chan struct{}
	logger   *output.Logger
}

// NewPool creates a new worker pool
func NewPool(cfg *config.Config, store queue.Store, namingPlugin *plugin.NamingPlugin, debugLogger *debug.Logger, logger *output.Logger) (*Pool, error) {
	executor, err := NewExecutor(cfg, namingPlugin, debugLogger)
	if err != nil {
		return nil, err
	}

	workers := cfg.Concurrency.MaxWorkers
	if workers <= 0 {
		workers = 4
	}

	return &Pool{
		cfg:      cfg,
		store:    store,
		executor: executor,
		workers:  workers,
		stopCh:   make(chan struct{}),
		logger:   logger,
	}, nil
}

// Start begins the worker pool
func (p *Pool) Start(ctx context.Context) {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}
}

// Stop stops the worker pool and waits for workers to finish
func (p *Pool) Stop() {
	close(p.stopCh)
	p.wg.Wait()
}

// worker is a single worker goroutine
func (p *Pool) worker(ctx context.Context, id int) {
	defer p.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopCh:
			return
		default:
		}

		// Try to dequeue a job
		job, err := p.store.Dequeue(ctx)
		if err != nil {
			p.logger.Error("dequeue failed: %v", err)
			time.Sleep(time.Second)
			continue
		}

		if job == nil {
			// No jobs available, wait a bit
			time.Sleep(100 * time.Millisecond)
			continue
		}

		p.processJob(ctx, job, id)
	}
}

func (p *Pool) processJob(ctx context.Context, job *queue.Job, workerID int) {
	p.logger.Processing(job.InputPath)

	result := p.executor.Execute(ctx, job)

	if result.Error != nil || result.ExitCode != 0 {
		if err := p.store.Fail(ctx, job.ID, result); err != nil {
			p.logger.Error("failed to mark job failed: %v", err)
		}
		p.logger.Failed(job.InputPath, result.ExitCode)
	} else {
		if err := p.store.Complete(ctx, job.ID, result); err != nil {
			p.logger.Error("failed to mark job complete: %v", err)
		}
		p.logger.Completed(job.InputPath, result.DurationMs)
	}
}

// RunOnce processes all pending jobs and returns
func (p *Pool) RunOnce(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		job, err := p.store.Dequeue(ctx)
		if err != nil {
			return fmt.Errorf("failed to dequeue: %w", err)
		}

		if job == nil {
			// No more jobs
			return nil
		}

		p.processJob(ctx, job, 0)
	}
}
