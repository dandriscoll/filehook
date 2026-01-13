package config

import (
	"fmt"
	"os"
)

// ValidationError represents a config validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("config.%s: %s", e.Field, e.Message)
}

// Validate checks the config for errors
func Validate(cfg *Config) []error {
	var errs []error

	// Version check
	if cfg.Version != 1 {
		errs = append(errs, ValidationError{
			Field:   "version",
			Message: fmt.Sprintf("unsupported version %d (only version 1 is supported)", cfg.Version),
		})
	}

	// Plugins validation
	if cfg.Plugins.FilenameGenerator == nil {
		errs = append(errs, ValidationError{
			Field:   "plugins.filename_generator",
			Message: "filename_generator plugin is required",
		})
	} else {
		if cfg.Plugins.FilenameGenerator.Path == "" {
			errs = append(errs, ValidationError{
				Field:   "plugins.filename_generator.path",
				Message: "path is required",
			})
		} else {
			pluginPath := cfg.ResolvePath(cfg.Plugins.FilenameGenerator.Path)
			if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
				errs = append(errs, ValidationError{
					Field:   "plugins.filename_generator.path",
					Message: fmt.Sprintf("plugin not found: %s", pluginPath),
				})
			}
		}
	}

	// Command validation
	if cfg.Command.AsString() == "" {
		errs = append(errs, ValidationError{
			Field:   "command",
			Message: "command is required",
		})
	}

	// Watch paths validation
	for i, p := range cfg.Watch.Paths {
		absPath := cfg.ResolvePath(p)
		info, err := os.Stat(absPath)
		if os.IsNotExist(err) {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("watch.paths[%d]", i),
				Message: fmt.Sprintf("path does not exist: %s", absPath),
			})
		} else if err == nil && !info.IsDir() {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("watch.paths[%d]", i),
				Message: fmt.Sprintf("path is not a directory: %s", absPath),
			})
		}
	}

	// Concurrency mode validation
	switch cfg.Concurrency.Mode {
	case ConcurrencyParallel, ConcurrencySequentialSwitch:
		// Valid
	default:
		errs = append(errs, ValidationError{
			Field:   "concurrency.mode",
			Message: fmt.Sprintf("invalid mode %q (must be 'parallel' or 'sequential_switch')", cfg.Concurrency.Mode),
		})
	}

	// On modified policy validation
	switch cfg.OnModified {
	case ModifiedIgnore, ModifiedReprocess, ModifiedIfNewer, ModifiedVersioned:
		// Valid
	default:
		errs = append(errs, ValidationError{
			Field:   "on_modified",
			Message: fmt.Sprintf("invalid policy %q (must be 'ignore', 'reprocess', 'if-newer', or 'versioned')", cfg.OnModified),
		})
	}

	// Inputs patterns validation
	if len(cfg.Inputs.Patterns) == 0 {
		errs = append(errs, ValidationError{
			Field:   "inputs.patterns",
			Message: "at least one input pattern is required",
		})
	}

	return errs
}
