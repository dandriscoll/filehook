package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultConfigName is the default config filename to search for
	DefaultConfigName = "filehook.yaml"
	// AltConfigName is an alternative config filename
	AltConfigName = "filehook.yml"
)

// Loader handles config discovery and loading
type Loader struct {
	configName string
}

// NewLoader creates a new config loader
func NewLoader(configName string) *Loader {
	if configName == "" {
		configName = DefaultConfigName
	}
	return &Loader{configName: configName}
}

// Discover walks up from the given directory to find the config file
func (l *Loader) Discover(startDir string) (string, error) {
	if startDir == "" {
		var err error
		startDir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current directory: %w", err)
		}
	}

	// Convert to absolute path
	absDir, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	currentDir := absDir
	for {
		// Try primary config name
		configPath := filepath.Join(currentDir, l.configName)
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}

		// Try alternative config name if using default
		if l.configName == DefaultConfigName {
			altPath := filepath.Join(currentDir, AltConfigName)
			if _, err := os.Stat(altPath); err == nil {
				return altPath, nil
			}
		}

		// Move to parent directory
		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir {
			// Reached root
			return "", fmt.Errorf("config file %q not found (searched from %s to root)", l.configName, absDir)
		}
		currentDir = parentDir
	}
}

// Load loads the config from the given path
func (l *Loader) Load(configPath string) (*Config, error) {
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	cfg.ConfigPath = absPath
	cfg.ConfigDir = filepath.Dir(absPath)

	// Apply defaults
	applyDefaults(cfg)

	return cfg, nil
}

// DiscoverAndLoad finds and loads the config in one step
func (l *Loader) DiscoverAndLoad(startDir string) (*Config, error) {
	configPath, err := l.Discover(startDir)
	if err != nil {
		return nil, err
	}
	return l.Load(configPath)
}

// applyDefaults sets default values for unset config fields
func applyDefaults(cfg *Config) {
	if cfg.Version == 0 {
		cfg.Version = 1
	}

	if cfg.Watch.DebounceMs <= 0 {
		cfg.Watch.DebounceMs = 200
	}

	if cfg.Concurrency.Mode == "" {
		cfg.Concurrency.Mode = ConcurrencyParallel
	}

	if cfg.Concurrency.MaxWorkers <= 0 {
		cfg.Concurrency.MaxWorkers = 4
	}

	if cfg.OnModified == "" {
		cfg.OnModified = ModifiedIfNewer
	}

	if cfg.StateDir == "" {
		cfg.StateDir = ".filehook"
	}
}
