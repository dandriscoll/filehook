package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/dandriscoll/filehook/internal/config"
	"github.com/dandriscoll/filehook/internal/debug"
)

// ShouldProcessInput is the JSON input for the should_process plugin
type ShouldProcessInput struct {
	Input        string             `json:"input"`
	Outputs      []string           `json:"outputs"`
	InputMtime   *time.Time         `json:"input_mtime"`
	OutputMtimes map[string]*string `json:"output_mtimes"` // null if file doesn't exist
}

// ShouldProcessOutput is the expected JSON output from the should_process plugin
type ShouldProcessOutput struct {
	Process bool   `json:"process"`
	Reason  string `json:"reason,omitempty"`
}

// ShouldProcessChecker determines whether to process a file based on modification policy.
//
// IMPORTANT: This is NOT for checking file readiness. For readiness checks, use the
// naming plugin (via ReadyChecker). The distinction is:
//
//   - ReadyChecker (naming plugin): "Is the external source ready for transformation?"
//     Example: Is the upstream system done writing the file?
//
//   - ShouldProcessChecker: "Given our policy, should we process this file?"
//     Example: Does the output already exist? Is the input newer than outputs?
//
// ShouldProcessChecker considers timestamps, output existence, and modification policy
// (ignore, reprocess, if-newer, versioned) to decide whether processing is needed.
type ShouldProcessChecker struct {
	runner *Runner
	policy config.ModifiedPolicy
}

// NewShouldProcessChecker creates a new should_process plugin wrapper
func NewShouldProcessChecker(cfg *config.Config, debugLogger *debug.Logger) *ShouldProcessChecker {
	var runner *Runner
	if cfg.Plugins.ShouldProcess != nil {
		pluginPath := cfg.ResolvePath(cfg.Plugins.ShouldProcess.Path)
		runner = NewRunner(pluginPath, cfg.Plugins.ShouldProcess.Args, cfg.ConfigDir)
		if debugLogger != nil {
			runner.WithDebugLogger(debugLogger)
		}
	}

	return &ShouldProcessChecker{
		runner: runner,
		policy: cfg.OnModified,
	}
}

// Check determines whether a file should be processed
func (sp *ShouldProcessChecker) Check(ctx context.Context, inputPath string, outputPaths []string, isModification bool) (bool, string, error) {
	// If we have a custom plugin, use it
	if sp.runner != nil {
		return sp.checkWithPlugin(ctx, inputPath, outputPaths)
	}

	// Otherwise, use default logic based on policy
	return sp.checkDefault(inputPath, outputPaths, isModification)
}

func (sp *ShouldProcessChecker) checkWithPlugin(ctx context.Context, inputPath string, outputPaths []string) (bool, string, error) {
	input := ShouldProcessInput{
		Input:        inputPath,
		Outputs:      outputPaths,
		OutputMtimes: make(map[string]*string),
	}

	// Get input mtime
	if info, err := os.Stat(inputPath); err == nil {
		mtime := info.ModTime()
		input.InputMtime = &mtime
	}

	// Get output mtimes
	for _, outPath := range outputPaths {
		if info, err := os.Stat(outPath); err == nil {
			mtime := info.ModTime().Format(time.RFC3339)
			input.OutputMtimes[outPath] = &mtime
		} else {
			input.OutputMtimes[outPath] = nil
		}
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return false, "", fmt.Errorf("failed to marshal plugin input: %w", err)
	}

	result, err := sp.runner.RunWithStdin(ctx, inputJSON)
	if err != nil {
		return false, "", fmt.Errorf("plugin execution failed: %w", err)
	}

	if result.ExitCode != 0 {
		return false, "", fmt.Errorf("plugin failed with exit code %d: %s", result.ExitCode, result.Stderr)
	}

	var output ShouldProcessOutput
	if err := json.Unmarshal([]byte(result.Stdout), &output); err != nil {
		return false, "", fmt.Errorf("failed to parse plugin output: %w", err)
	}

	return output.Process, output.Reason, nil
}

func (sp *ShouldProcessChecker) checkDefault(inputPath string, outputPaths []string, isModification bool) (bool, string, error) {
	// Get input file info
	inputInfo, err := os.Stat(inputPath)
	if err != nil {
		return false, "", fmt.Errorf("failed to stat input file: %w", err)
	}

	// Check if any output is missing
	allOutputsExist := true
	var oldestOutputMtime time.Time
	for i, outPath := range outputPaths {
		info, err := os.Stat(outPath)
		if os.IsNotExist(err) {
			allOutputsExist = false
			break
		} else if err != nil {
			return false, "", fmt.Errorf("failed to stat output file: %w", err)
		}
		if i == 0 || info.ModTime().Before(oldestOutputMtime) {
			oldestOutputMtime = info.ModTime()
		}
	}

	// If outputs don't exist, always process
	if !allOutputsExist {
		return true, "output missing", nil
	}

	// Outputs exist - apply policy
	if !isModification {
		// This is a new file detection but outputs already exist
		// (e.g., from a previous run)
		return false, "outputs already exist", nil
	}

	// This is a modification
	switch sp.policy {
	case config.ModifiedIgnore:
		return false, "ignore policy", nil

	case config.ModifiedReprocess:
		return true, "reprocess policy", nil

	case config.ModifiedIfNewer:
		if inputInfo.ModTime().After(oldestOutputMtime) {
			return true, "input newer than outputs", nil
		}
		return false, "outputs up to date", nil

	case config.ModifiedVersioned:
		// Always process for versioned - the executor will handle versioning
		return true, "versioned policy", nil

	default:
		return false, "unknown policy", nil
	}
}
