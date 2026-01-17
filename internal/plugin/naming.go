package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/dandriscoll/filehook/internal/config"
)

// NamingOutput is the expected JSON output from the naming plugin
type NamingOutput struct {
	Outputs []string `json:"outputs"`
	Ready   *bool    `json:"ready,omitempty"` // Optional: if false, file is not ready for processing
}

// NamingResult contains both the generated outputs and the ready status
type NamingResult struct {
	Outputs []string // Resolved absolute output paths
	Ready   bool     // Whether the file is ready for processing (defaults to true if not specified)
}

// NamingPlugin wraps the naming plugin which handles both filename generation
// and readiness checks. These are combined because they're closely related -
// both depend on understanding the external source state.
//
// The plugin returns JSON with:
//   - "outputs": array of output filenames (required)
//   - "ready": boolean indicating if file is ready (optional, defaults to true)
//
// NOTE: The "ready" check is NOT the same as ShouldProcessChecker.
// Ready asks "is the external source ready?" (e.g., is upstream done writing?)
// ShouldProcess asks "should we process based on our policy?" (e.g., are outputs stale?)
type NamingPlugin struct {
	runner     *Runner
	outputRoot string
}

// NewNamingPlugin creates a new naming plugin wrapper
func NewNamingPlugin(cfg *config.Config) (*NamingPlugin, error) {
	if cfg.Plugins.Naming == nil {
		return nil, fmt.Errorf("naming plugin not configured")
	}

	pluginPath := cfg.ResolvePath(cfg.Plugins.Naming.Path)

	return &NamingPlugin{
		runner:     NewRunner(pluginPath, cfg.Plugins.Naming.Args, cfg.ConfigDir),
		outputRoot: cfg.OutputRoot(),
	}, nil
}

// Generate calls the plugin to generate output filenames and check readiness for an input file.
// Returns outputs and ready status. If the plugin doesn't specify "ready", it defaults to true.
func (np *NamingPlugin) Generate(ctx context.Context, inputPath string) (*NamingResult, error) {
	result, err := np.runner.Run(ctx, inputPath)
	if err != nil {
		return nil, fmt.Errorf("plugin execution failed: %w", err)
	}

	if result.ExitCode != 0 {
		return nil, fmt.Errorf("plugin failed with exit code %d: %s", result.ExitCode, result.Stderr)
	}

	var output NamingOutput
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
			resolved[i] = filepath.Join(np.outputRoot, p)
		}
	}

	// Default ready to true if not specified
	ready := true
	if output.Ready != nil {
		ready = *output.Ready
	}

	return &NamingResult{
		Outputs: resolved,
		Ready:   ready,
	}, nil
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
