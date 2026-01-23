package plugin

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dandriscoll/filehook/internal/config"
	"github.com/dandriscoll/filehook/internal/debug"
)

// NamingResult contains both the generated outputs and the ready status
type NamingResult struct {
	Outputs []string // Resolved absolute output paths
	Ready   bool     // Whether the file is ready for processing (defaults to true if not specified)
}

// NamingPlugin wraps the naming plugin which handles filename generation
// and readiness checks via separate operations.
//
// The plugin follows the ft tool contract:
//   - "ready <path>" returns "true" or "false" (plain text)
//   - "propose <path> <type>" returns the proposed output filename (plain text)
//
// NOTE: The "ready" check is NOT the same as ShouldProcessChecker.
// Ready asks "is the external source ready?" (e.g., is upstream done writing?)
// ShouldProcess asks "should we process based on our policy?" (e.g., are outputs stale?)
type NamingPlugin struct {
	runner     *Runner
	outputRoot string
}

// NewNamingPlugin creates a new naming plugin wrapper
func NewNamingPlugin(cfg *config.Config, debugLogger *debug.Logger) (*NamingPlugin, error) {
	if cfg.Plugins.Naming == nil {
		return nil, fmt.Errorf("naming plugin not configured")
	}

	pluginPath := cfg.ResolvePath(cfg.Plugins.Naming.Path)
	runner := NewRunner(pluginPath, cfg.Plugins.Naming.Args, cfg.ConfigDir)
	if debugLogger != nil {
		runner.WithDebugLogger(debugLogger)
	}

	return &NamingPlugin{
		runner:     runner,
		outputRoot: cfg.OutputRoot(),
	}, nil
}

// Generate calls the plugin to generate output filenames and check readiness for an input file.
// targetType specifies the output type (e.g., "png", "sdxl") for the naming plugin.
// Returns outputs and ready status.
func (np *NamingPlugin) Generate(ctx context.Context, inputPath string, targetType string) (*NamingResult, error) {
	// First, check if file is ready using "ready <path>"
	readyResult, err := np.runner.Run(ctx, "ready", inputPath)
	if err != nil {
		return nil, fmt.Errorf("ready check failed: %w", err)
	}

	if readyResult.ExitCode != 0 {
		return nil, fmt.Errorf("ready check failed with exit code %d: %s", readyResult.ExitCode, readyResult.Stderr)
	}

	ready := strings.TrimSpace(readyResult.Stdout) == "true"

	// Then, get proposed output filename using "propose <path> <type>"
	outputs, err := np.Propose(ctx, inputPath, targetType)
	if err != nil {
		return nil, err
	}

	return &NamingResult{
		Outputs: outputs,
		Ready:   ready,
	}, nil
}

// Propose calls the plugin to get output filenames for an input file.
// This is called at execution time to calculate the actual output paths.
func (np *NamingPlugin) Propose(ctx context.Context, inputPath string, targetType string) ([]string, error) {
	proposeResult, err := np.runner.Run(ctx, "propose", inputPath, targetType)
	if err != nil {
		return nil, fmt.Errorf("propose failed: %w", err)
	}

	if proposeResult.ExitCode != 0 {
		return nil, fmt.Errorf("propose failed with exit code %d: %s", proposeResult.ExitCode, proposeResult.Stderr)
	}

	proposedPath := strings.TrimSpace(proposeResult.Stdout)
	if proposedPath == "" {
		return nil, fmt.Errorf("propose returned empty output")
	}

	// Resolve relative paths against output root
	var resolved string
	if filepath.IsAbs(proposedPath) {
		resolved = proposedPath
	} else {
		resolved = filepath.Join(np.outputRoot, proposedPath)
	}

	return []string{resolved}, nil
}

// GroupKeyGenerator wraps the group key plugin
type GroupKeyGenerator struct {
	runner *Runner
}

// NewGroupKeyGenerator creates a new group key plugin wrapper
func NewGroupKeyGenerator(cfg *config.Config, debugLogger *debug.Logger) *GroupKeyGenerator {
	if cfg.Plugins.GroupKey == nil {
		return nil
	}

	pluginPath := cfg.ResolvePath(cfg.Plugins.GroupKey.Path)
	runner := NewRunner(pluginPath, cfg.Plugins.GroupKey.Args, cfg.ConfigDir)
	if debugLogger != nil {
		runner.WithDebugLogger(debugLogger)
	}

	return &GroupKeyGenerator{
		runner: runner,
	}
}

// Generate calls the plugin to generate a group key for an input file
func (gk *GroupKeyGenerator) Generate(ctx context.Context, inputPath string) (string, error) {
	result, err := gk.runner.Run(ctx, inputPath)
	if err != nil {
		return "", fmt.Errorf("plugin execution failed: %w", err)
	}

	if result.ExitCode != 0 {
		return "", fmt.Errorf("plugin failed with exit code %d: %s", result.ExitCode, result.Stderr)
	}

	// Output is a single line
	return trimNewline(result.Stdout), nil
}

// DefaultGroupKey returns a default group key based on the first directory component
func DefaultGroupKey(inputPath, baseDir string) string {
	relPath, err := filepath.Rel(baseDir, inputPath)
	if err != nil {
		return ""
	}

	// Get first directory component
	parts := filepath.SplitList(relPath)
	if len(parts) > 0 {
		first := filepath.Dir(relPath)
		if first != "." {
			// Get just the first component
			for {
				parent := filepath.Dir(first)
				if parent == "." || parent == first {
					return first
				}
				first = parent
			}
		}
	}

	// No subdirectory, use extension as group
	return filepath.Ext(inputPath)
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
