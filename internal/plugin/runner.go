package plugin

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// Result holds the result of a plugin execution
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// Runner executes plugin scripts
type Runner struct {
	path    string
	args    []string
	workDir string
	timeout time.Duration
}

// NewRunner creates a new plugin runner
func NewRunner(path string, args []string, workDir string) *Runner {
	return &Runner{
		path:    path,
		args:    args,
		workDir: workDir,
		timeout: 30 * time.Second,
	}
}

// WithTimeout sets the timeout for plugin execution
func (r *Runner) WithTimeout(d time.Duration) *Runner {
	r.timeout = d
	return r
}

// Run executes the plugin with additional arguments
func (r *Runner) Run(ctx context.Context, extraArgs ...string) (*Result, error) {
	args := make([]string, 0, len(r.args)+len(extraArgs))
	args = append(args, r.args...)
	args = append(args, extraArgs...)

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, r.path, args...)
	cmd.Dir = r.workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	result := &Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return result, fmt.Errorf("failed to run plugin: %w", err)
		}
	}

	return result, nil
}

// RunWithStdin executes the plugin with stdin input
func (r *Runner) RunWithStdin(ctx context.Context, stdin []byte, extraArgs ...string) (*Result, error) {
	args := make([]string, 0, len(r.args)+len(extraArgs))
	args = append(args, r.args...)
	args = append(args, extraArgs...)

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, r.path, args...)
	cmd.Dir = r.workDir
	cmd.Stdin = bytes.NewReader(stdin)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	result := &Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return result, fmt.Errorf("failed to run plugin: %w", err)
		}
	}

	return result, nil
}
