package plugin

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/dandriscoll/filehook/internal/debug"
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
	path        string
	args        []string
	workDir     string
	timeout     time.Duration
	debugLogger *debug.Logger
	pluginName  string
}

// NewRunner creates a new plugin runner
func NewRunner(path string, args []string, workDir string) *Runner {
	return &Runner{
		path:       path,
		args:       args,
		workDir:    workDir,
		timeout:    30 * time.Second,
		pluginName: filepath.Base(path),
	}
}

// WithDebugLogger sets the debug logger for this runner
func (r *Runner) WithDebugLogger(logger *debug.Logger) *Runner {
	r.debugLogger = logger
	return r
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
			r.logPluginCall(extraArgs, "", result)
			return result, fmt.Errorf("failed to run plugin: %w", err)
		}
	}

	r.logPluginCall(extraArgs, "", result)
	return result, nil
}

func (r *Runner) logPluginCall(args []string, stdin string, result *Result) {
	if r.debugLogger == nil || !r.debugLogger.Enabled() {
		return
	}
	operation := ""
	if len(args) > 0 {
		operation = args[0]
	}
	r.debugLogger.PluginCall(r.pluginName, operation, args, stdin, result.Stdout, result.Stderr, result.ExitCode, result.Duration)
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
			r.logPluginCall(extraArgs, string(stdin), result)
			return result, fmt.Errorf("failed to run plugin: %w", err)
		}
	}

	r.logPluginCall(extraArgs, string(stdin), result)
	return result, nil
}
