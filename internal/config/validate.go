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

	// Plugins validation - naming plugin handles both filename generation AND ready checks
	if cfg.Plugins.Naming == nil {
		errs = append(errs, ValidationError{
			Field:   "plugins.naming",
			Message: "naming plugin is required (handles filename generation and ready checks)",
		})
	} else {
		if cfg.Plugins.Naming.Path == "" {
			errs = append(errs, ValidationError{
				Field:   "plugins.naming.path",
				Message: "path is required",
			})
		} else {
			pluginPath := cfg.ResolvePath(cfg.Plugins.Naming.Path)
			if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
				errs = append(errs, ValidationError{
					Field:   "plugins.naming.path",
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
	case ConcurrencyParallel, ConcurrencySequentialSwitch, ConcurrencyStack:
		// Valid
	default:
		errs = append(errs, ValidationError{
			Field:   "concurrency.mode",
			Message: fmt.Sprintf("invalid mode %q (must be 'parallel', 'sequential_switch', or 'stack')", cfg.Concurrency.Mode),
		})
	}

	// Stack mode validation
	if cfg.Concurrency.Mode == ConcurrencyStack {
		errs = append(errs, validateStackMode(cfg)...)
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

	// Validate pattern names are unique (when provided)
	seenNames := make(map[string]int)
	for i, p := range cfg.Inputs.Patterns {
		if p.Name != "" {
			if prevIdx, exists := seenNames[p.Name]; exists {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("inputs.patterns[%d].name", i),
					Message: fmt.Sprintf("duplicate pattern name %q (also used at index %d)", p.Name, prevIdx),
				})
			}
			seenNames[p.Name] = i
		}
	}

	return errs
}

// validateStackMode validates stack mode specific configuration
func validateStackMode(cfg *Config) []error {
	var errs []error

	// Require stack definitions
	if len(cfg.Stacks.Definitions) == 0 {
		errs = append(errs, ValidationError{
			Field:   "stacks.definitions",
			Message: "at least one stack definition is required when using stack mode",
		})
		return errs // Can't validate further without definitions
	}

	// Build set of defined stack names
	stackNames := make(map[string]bool)
	for i, def := range cfg.Stacks.Definitions {
		if def.Name == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("stacks.definitions[%d].name", i),
				Message: "stack name is required",
			})
			continue
		}

		if stackNames[def.Name] {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("stacks.definitions[%d].name", i),
				Message: fmt.Sprintf("duplicate stack name %q", def.Name),
			})
		}
		stackNames[def.Name] = true

		switchCmd := def.GetSwitchCommand()
		if len(switchCmd) == 0 {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("stacks.definitions[%d]", i),
				Message: "switch_command or switch_script is required",
			})
		} else {
			// Validate the executable exists and is executable
			execPath := cfg.ResolvePath(switchCmd[0])
			info, err := os.Stat(execPath)
			if os.IsNotExist(err) {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("stacks.definitions[%d].switch_command", i),
					Message: fmt.Sprintf("executable not found: %s", execPath),
				})
			} else if err == nil {
				// Check if executable (Unix permissions)
				if info.Mode()&0111 == 0 {
					errs = append(errs, ValidationError{
						Field:   fmt.Sprintf("stacks.definitions[%d].switch_command", i),
						Message: fmt.Sprintf("file is not executable: %s", execPath),
					})
				}
			}
		}
	}

	// Validate default stack if set
	if cfg.Stacks.Default != "" && !stackNames[cfg.Stacks.Default] {
		errs = append(errs, ValidationError{
			Field:   "stacks.default",
			Message: fmt.Sprintf("default stack %q is not defined in stacks.definitions", cfg.Stacks.Default),
		})
	}

	// Validate pattern stack references
	for i, pattern := range cfg.Inputs.Patterns {
		if pattern.Stack != "" && !stackNames[pattern.Stack] {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("inputs.patterns[%d].stack", i),
				Message: fmt.Sprintf("stack %q is not defined in stacks.definitions", pattern.Stack),
			})
		}
	}

	return errs
}
