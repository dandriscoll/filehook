package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/anthropics/filehook/internal/config"
	"github.com/anthropics/filehook/internal/queue"
)

// Executor runs jobs
type Executor struct {
	cfg     *config.Config
	tempDir string
}

// NewExecutor creates a new executor
func NewExecutor(cfg *config.Config) (*Executor, error) {
	tempDir := filepath.Join(cfg.StateDirectory(), "tmp")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	return &Executor{
		cfg:     cfg,
		tempDir: tempDir,
	}, nil
}

// Execute runs a job and returns the result
func (e *Executor) Execute(ctx context.Context, job *queue.Job) *queue.JobResult {
	start := time.Now()

	// Build the command
	cmdArgs := e.buildCommand(job)
	if len(cmdArgs) == 0 {
		return &queue.JobResult{
			ExitCode: 1,
			Error:    fmt.Errorf("empty command"),
		}
	}

	// Ensure output directories exist
	for _, outPath := range job.OutputPaths {
		outDir := filepath.Dir(outPath)
		if err := os.MkdirAll(outDir, 0755); err != nil {
			return &queue.JobResult{
				ExitCode:   1,
				DurationMs: time.Since(start).Milliseconds(),
				Error:      fmt.Errorf("failed to create output directory: %w", err),
			}
		}
	}

	// Execute the command
	var stdout, stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	cmd.Dir = e.cfg.ConfigDir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = e.buildEnv(job)

	err := cmd.Run()
	duration := time.Since(start)

	result := &queue.JobResult{
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		DurationMs: duration.Milliseconds(),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			result.Error = fmt.Errorf("command exited with code %d", result.ExitCode)
		} else {
			result.ExitCode = 1
			result.Error = err
		}
	}

	return result
}

// buildCommand builds the command with variable substitution
func (e *Executor) buildCommand(job *queue.Job) []string {
	cmdSlice := e.cfg.Command.AsSlice()
	if len(cmdSlice) == 0 {
		return nil
	}

	// Build substitution map
	subs := map[string]string{
		"{{input}}":      job.InputPath,
		"{{output}}":     "",
		"{{outputs}}":    strings.Join(job.OutputPaths, " "),
		"{{output_dir}}": "",
	}

	if len(job.OutputPaths) > 0 {
		subs["{{output}}"] = job.OutputPaths[0]
		subs["{{output_dir}}"] = filepath.Dir(job.OutputPaths[0])
	}

	// JSON outputs
	outputsJSON, _ := json.Marshal(job.OutputPaths)
	subs["{{outputs_json}}"] = string(outputsJSON)

	// Apply substitutions
	result := make([]string, len(cmdSlice))
	for i, arg := range cmdSlice {
		result[i] = e.substitute(arg, subs)
	}

	return result
}

// substitute replaces template variables in a string
func (e *Executor) substitute(s string, subs map[string]string) string {
	for k, v := range subs {
		s = strings.ReplaceAll(s, k, v)
	}
	return s
}

// buildEnv builds environment variables for the command
func (e *Executor) buildEnv(job *queue.Job) []string {
	env := os.Environ()

	// Add filehook-specific variables
	env = append(env,
		fmt.Sprintf("FILEHOOK_INPUT=%s", job.InputPath),
		fmt.Sprintf("FILEHOOK_OUTPUT=%s", strings.Join(job.OutputPaths, ":")),
		fmt.Sprintf("FILEHOOK_OUTPUT_DIR=%s", filepath.Dir(job.OutputPaths[0])),
		fmt.Sprintf("FILEHOOK_JOB_ID=%s", job.ID),
	)

	return env
}

// VersionedOutputPaths returns versioned output paths for the versioned policy
func VersionedOutputPaths(originalPaths []string, version int) []string {
	result := make([]string, len(originalPaths))
	for i, p := range originalPaths {
		dir := filepath.Dir(p)
		base := filepath.Base(p)
		ext := filepath.Ext(base)
		name := strings.TrimSuffix(base, ext)

		versionedName := fmt.Sprintf("%s.v%d%s", name, version, ext)
		result[i] = filepath.Join(dir, versionedName)
	}
	return result
}
