package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/dandriscoll/filehook/internal/config"
)

// FilenameGeneratorOutput is the expected JSON output from the filename generator plugin
type FilenameGeneratorOutput struct {
	Outputs []string `json:"outputs"`
}

// ReadyChecker checks whether a file is ready for transformation using the naming tool
type ReadyChecker struct {
	runner *Runner
}

// NewReadyChecker creates a new ready checker using the naming tool
func NewReadyChecker(cfg *config.Config) (*ReadyChecker, error) {
	if cfg.Plugins.FilenameGenerator == nil {
		return nil, fmt.Errorf("filename_generator plugin not configured")
	}

	pluginPath := cfg.ResolvePath(cfg.Plugins.FilenameGenerator.Path)

	// The ready command uses the same plugin path but with "ready" as the first arg
	return &ReadyChecker{
		runner: NewRunner(pluginPath, []string{"ready"}, cfg.ConfigDir),
	}, nil
}

// Check returns true if the file is ready for transformation
func (rc *ReadyChecker) Check(ctx context.Context, inputPath string) (bool, error) {
	result, err := rc.runner.Run(ctx, inputPath)
	if err != nil {
		return false, fmt.Errorf("ready check failed: %w", err)
	}

	if result.ExitCode != 0 {
		return false, fmt.Errorf("ready check failed with exit code %d: %s", result.ExitCode, result.Stderr)
	}

	output := trimNewline(result.Stdout)
	return output == "true", nil
}

// FilenameGenerator wraps the filename generator plugin
type FilenameGenerator struct {
	runner     *Runner
	outputRoot string
}

// NewFilenameGenerator creates a new filename generator plugin wrapper
func NewFilenameGenerator(cfg *config.Config) (*FilenameGenerator, error) {
	if cfg.Plugins.FilenameGenerator == nil {
		return nil, fmt.Errorf("filename_generator plugin not configured")
	}

	pluginPath := cfg.ResolvePath(cfg.Plugins.FilenameGenerator.Path)

	return &FilenameGenerator{
		runner:     NewRunner(pluginPath, cfg.Plugins.FilenameGenerator.Args, cfg.ConfigDir),
		outputRoot: cfg.OutputRoot(),
	}, nil
}

// Generate calls the plugin to generate output filenames for an input file
func (fg *FilenameGenerator) Generate(ctx context.Context, inputPath string) ([]string, error) {
	result, err := fg.runner.Run(ctx, inputPath)
	if err != nil {
		return nil, fmt.Errorf("plugin execution failed: %w", err)
	}

	if result.ExitCode != 0 {
		return nil, fmt.Errorf("plugin failed with exit code %d: %s", result.ExitCode, result.Stderr)
	}

	var output FilenameGeneratorOutput
	if err := json.Unmarshal([]byte(result.Stdout), &output); err != nil {
		return nil, fmt.Errorf("failed to parse plugin output: %w (output: %s)", err, result.Stdout)
	}

	if len(output.Outputs) == 0 {
		return nil, fmt.Errorf("plugin returned no outputs")
	}

	// Resolve relative paths against output root
	resolved := make([]string, len(output.Outputs))
	for i, p := range output.Outputs {
		if filepath.IsAbs(p) {
			resolved[i] = p
		} else {
			resolved[i] = filepath.Join(fg.outputRoot, p)
		}
	}

	return resolved, nil
}

// GroupKeyGenerator wraps the group key plugin
type GroupKeyGenerator struct {
	runner *Runner
}

// NewGroupKeyGenerator creates a new group key plugin wrapper
func NewGroupKeyGenerator(cfg *config.Config) *GroupKeyGenerator {
	if cfg.Plugins.GroupKey == nil {
		return nil
	}

	pluginPath := cfg.ResolvePath(cfg.Plugins.GroupKey.Path)

	return &GroupKeyGenerator{
		runner: NewRunner(pluginPath, cfg.Plugins.GroupKey.Args, cfg.ConfigDir),
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
