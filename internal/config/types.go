package config

import (
	"path/filepath"
	"time"
)

// Config represents the full filehook configuration
type Config struct {
	Version     int               `yaml:"version"`
	Watch       WatchConfig       `yaml:"watch"`
	Inputs      InputsConfig      `yaml:"inputs"`
	Outputs     OutputsConfig     `yaml:"outputs"`
	Command     CommandConfig     `yaml:"command"`
	Plugins     PluginsConfig     `yaml:"plugins"`
	Concurrency ConcurrencyConfig `yaml:"concurrency"`
	OnModified  ModifiedPolicy    `yaml:"on_modified"`
	StateDir    string            `yaml:"state_dir"`

	// ConfigPath is the absolute path to the config file (set during load)
	ConfigPath string `yaml:"-"`
	// ConfigDir is the directory containing the config file
	ConfigDir string `yaml:"-"`
}

// WatchConfig defines what paths to watch
type WatchConfig struct {
	Paths      []string      `yaml:"paths"`
	Ignore     []string      `yaml:"ignore"`
	DebounceMs int           `yaml:"debounce_ms"`
}

// DebounceDuration returns the debounce duration
func (w WatchConfig) DebounceDuration() time.Duration {
	if w.DebounceMs <= 0 {
		return 200 * time.Millisecond
	}
	return time.Duration(w.DebounceMs) * time.Millisecond
}

// InputsConfig defines which files to process
type InputsConfig struct {
	Patterns []PatternConfig `yaml:"patterns"`
}

// PatternConfig defines a single input pattern with optional exclusions
type PatternConfig struct {
	Pattern string   `yaml:"pattern"`
	Exclude []string `yaml:"exclude"`
}

// UnmarshalYAML handles both string and object pattern formats
func (p *PatternConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try string first (simple pattern)
	var s string
	if err := unmarshal(&s); err == nil {
		p.Pattern = s
		p.Exclude = nil
		return nil
	}
	// Try object format
	type patternConfigRaw struct {
		Pattern string   `yaml:"pattern"`
		Exclude []string `yaml:"exclude"`
	}
	var raw patternConfigRaw
	if err := unmarshal(&raw); err != nil {
		return err
	}
	p.Pattern = raw.Pattern
	p.Exclude = raw.Exclude
	return nil
}

// OutputsConfig defines where outputs go
type OutputsConfig struct {
	Root string `yaml:"root"`
}

// CommandConfig can be a string or list of strings
type CommandConfig struct {
	raw interface{}
}

// UnmarshalYAML handles both string and []string command formats
func (c *CommandConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try string first
	var s string
	if err := unmarshal(&s); err == nil {
		c.raw = s
		return nil
	}
	// Try []string
	var arr []string
	if err := unmarshal(&arr); err == nil {
		c.raw = arr
		return nil
	}
	return nil
}

// AsString returns the command as a single string (for shell execution)
func (c CommandConfig) AsString() string {
	switch v := c.raw.(type) {
	case string:
		return v
	case []string:
		// Join with spaces (simple approach)
		result := ""
		for i, s := range v {
			if i > 0 {
				result += " "
			}
			result += s
		}
		return result
	default:
		return ""
	}
}

// AsSlice returns the command as a slice of strings
func (c CommandConfig) AsSlice() []string {
	switch v := c.raw.(type) {
	case string:
		return []string{"sh", "-c", v}
	case []string:
		return v
	default:
		return nil
	}
}

// PluginsConfig defines the plugin configurations
type PluginsConfig struct {
	FilenameGenerator *PluginConfig `yaml:"filename_generator"`
	ShouldProcess     *PluginConfig `yaml:"should_process"`
	GroupKey          *PluginConfig `yaml:"group_key"`
}

// PluginConfig defines a single plugin
type PluginConfig struct {
	Path string   `yaml:"path"`
	Args []string `yaml:"args"`
}

// ConcurrencyConfig defines how jobs are processed
type ConcurrencyConfig struct {
	Mode       ConcurrencyMode `yaml:"mode"`
	MaxWorkers int             `yaml:"max_workers"`
}

// ConcurrencyMode defines parallel vs sequential processing
type ConcurrencyMode string

const (
	ConcurrencyParallel         ConcurrencyMode = "parallel"
	ConcurrencySequentialSwitch ConcurrencyMode = "sequential_switch"
)

// ModifiedPolicy defines behavior when input files are modified
type ModifiedPolicy string

const (
	ModifiedIgnore    ModifiedPolicy = "ignore"
	ModifiedReprocess ModifiedPolicy = "reprocess"
	ModifiedIfNewer   ModifiedPolicy = "if-newer"
	ModifiedVersioned ModifiedPolicy = "versioned"
)

// ResolvePath resolves a path relative to the config directory
func (c *Config) ResolvePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.ConfigDir, p)
}

// StateDirectory returns the absolute path to the state directory
func (c *Config) StateDirectory() string {
	if c.StateDir == "" {
		return filepath.Join(c.ConfigDir, ".filehook")
	}
	return c.ResolvePath(c.StateDir)
}

// OutputRoot returns the absolute path to the output root
func (c *Config) OutputRoot() string {
	if c.Outputs.Root == "" {
		return c.ConfigDir
	}
	return c.ResolvePath(c.Outputs.Root)
}

// WatchPaths returns absolute watch paths
func (c *Config) WatchPaths() []string {
	if len(c.Watch.Paths) == 0 {
		return []string{c.ConfigDir}
	}
	paths := make([]string, len(c.Watch.Paths))
	for i, p := range c.Watch.Paths {
		paths[i] = c.ResolvePath(p)
	}
	return paths
}
