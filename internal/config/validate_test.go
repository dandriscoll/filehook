package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateStackMode(t *testing.T) {
	// Create a temporary directory with test switch scripts
	tmpDir, err := os.MkdirTemp("", "filehook-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create executable switch scripts
	scriptA := filepath.Join(tmpDir, "switch-a.sh")
	scriptB := filepath.Join(tmpDir, "switch-b.sh")
	for _, script := range []string{scriptA, scriptB} {
		if err := os.WriteFile(script, []byte("#!/bin/bash\necho switching"), 0755); err != nil {
			t.Fatalf("failed to create script %s: %v", script, err)
		}
	}

	// Create a non-executable script
	nonExecScript := filepath.Join(tmpDir, "non-exec.sh")
	if err := os.WriteFile(nonExecScript, []byte("#!/bin/bash\necho switching"), 0644); err != nil {
		t.Fatalf("failed to create non-exec script: %v", err)
	}

	tests := []struct {
		name       string
		cfg        *Config
		wantErrors int
		errorField string // substring to check in first error
	}{
		{
			name: "valid stack config",
			cfg: &Config{
				Version:   1,
				ConfigDir: tmpDir,
				Command:   CommandConfig{raw: "echo test"},
				Plugins:   PluginsConfig{Naming: &PluginConfig{Path: scriptA}},
				Inputs:    InputsConfig{Patterns: []PatternConfig{{Pattern: "*.txt", Stack: "stack_a"}}},
				Concurrency: ConcurrencyConfig{
					Mode: ConcurrencyStack,
				},
				Stacks: StacksConfig{
					Definitions: []StackDefinition{
						{Name: "stack_a", SwitchScript: "switch-a.sh"},
						{Name: "stack_b", SwitchScript: "switch-b.sh"},
					},
				},
			},
			wantErrors: 0,
		},
		{
			name: "stack mode requires definitions",
			cfg: &Config{
				Version:   1,
				ConfigDir: tmpDir,
				Command:   CommandConfig{raw: "echo test"},
				Plugins:   PluginsConfig{Naming: &PluginConfig{Path: scriptA}},
				Inputs:    InputsConfig{Patterns: []PatternConfig{{Pattern: "*.txt"}}},
				Concurrency: ConcurrencyConfig{
					Mode: ConcurrencyStack,
				},
				Stacks: StacksConfig{
					Definitions: []StackDefinition{},
				},
			},
			wantErrors: 1,
			errorField: "stacks.definitions",
		},
		{
			name: "stack definition requires name",
			cfg: &Config{
				Version:   1,
				ConfigDir: tmpDir,
				Command:   CommandConfig{raw: "echo test"},
				Plugins:   PluginsConfig{Naming: &PluginConfig{Path: scriptA}},
				Inputs:    InputsConfig{Patterns: []PatternConfig{{Pattern: "*.txt"}}},
				Concurrency: ConcurrencyConfig{
					Mode: ConcurrencyStack,
				},
				Stacks: StacksConfig{
					Definitions: []StackDefinition{
						{Name: "", SwitchScript: "switch-a.sh"},
					},
				},
			},
			wantErrors: 1,
			errorField: "name",
		},
		{
			name: "stack definition requires switch_script",
			cfg: &Config{
				Version:   1,
				ConfigDir: tmpDir,
				Command:   CommandConfig{raw: "echo test"},
				Plugins:   PluginsConfig{Naming: &PluginConfig{Path: scriptA}},
				Inputs:    InputsConfig{Patterns: []PatternConfig{{Pattern: "*.txt"}}},
				Concurrency: ConcurrencyConfig{
					Mode: ConcurrencyStack,
				},
				Stacks: StacksConfig{
					Definitions: []StackDefinition{
						{Name: "stack_a", SwitchScript: ""},
					},
				},
			},
			wantErrors: 1,
			errorField: "switch_script",
		},
		{
			name: "switch script must exist",
			cfg: &Config{
				Version:   1,
				ConfigDir: tmpDir,
				Command:   CommandConfig{raw: "echo test"},
				Plugins:   PluginsConfig{Naming: &PluginConfig{Path: scriptA}},
				Inputs:    InputsConfig{Patterns: []PatternConfig{{Pattern: "*.txt"}}},
				Concurrency: ConcurrencyConfig{
					Mode: ConcurrencyStack,
				},
				Stacks: StacksConfig{
					Definitions: []StackDefinition{
						{Name: "stack_a", SwitchScript: "nonexistent.sh"},
					},
				},
			},
			wantErrors: 1,
			errorField: "not found",
		},
		{
			name: "switch script must be executable",
			cfg: &Config{
				Version:   1,
				ConfigDir: tmpDir,
				Command:   CommandConfig{raw: "echo test"},
				Plugins:   PluginsConfig{Naming: &PluginConfig{Path: scriptA}},
				Inputs:    InputsConfig{Patterns: []PatternConfig{{Pattern: "*.txt"}}},
				Concurrency: ConcurrencyConfig{
					Mode: ConcurrencyStack,
				},
				Stacks: StacksConfig{
					Definitions: []StackDefinition{
						{Name: "stack_a", SwitchScript: "non-exec.sh"},
					},
				},
			},
			wantErrors: 1,
			errorField: "not executable",
		},
		{
			name: "duplicate stack names not allowed",
			cfg: &Config{
				Version:   1,
				ConfigDir: tmpDir,
				Command:   CommandConfig{raw: "echo test"},
				Plugins:   PluginsConfig{Naming: &PluginConfig{Path: scriptA}},
				Inputs:    InputsConfig{Patterns: []PatternConfig{{Pattern: "*.txt"}}},
				Concurrency: ConcurrencyConfig{
					Mode: ConcurrencyStack,
				},
				Stacks: StacksConfig{
					Definitions: []StackDefinition{
						{Name: "stack_a", SwitchScript: "switch-a.sh"},
						{Name: "stack_a", SwitchScript: "switch-b.sh"},
					},
				},
			},
			wantErrors: 1,
			errorField: "duplicate",
		},
		{
			name: "pattern stack must reference defined stack",
			cfg: &Config{
				Version:   1,
				ConfigDir: tmpDir,
				Command:   CommandConfig{raw: "echo test"},
				Plugins:   PluginsConfig{Naming: &PluginConfig{Path: scriptA}},
				Inputs:    InputsConfig{Patterns: []PatternConfig{{Pattern: "*.txt", Stack: "undefined_stack"}}},
				Concurrency: ConcurrencyConfig{
					Mode: ConcurrencyStack,
				},
				Stacks: StacksConfig{
					Definitions: []StackDefinition{
						{Name: "stack_a", SwitchScript: "switch-a.sh"},
					},
				},
			},
			wantErrors: 1,
			errorField: "not defined",
		},
		{
			name: "default stack must reference defined stack",
			cfg: &Config{
				Version:   1,
				ConfigDir: tmpDir,
				Command:   CommandConfig{raw: "echo test"},
				Plugins:   PluginsConfig{Naming: &PluginConfig{Path: scriptA}},
				Inputs:    InputsConfig{Patterns: []PatternConfig{{Pattern: "*.txt"}}},
				Concurrency: ConcurrencyConfig{
					Mode: ConcurrencyStack,
				},
				Stacks: StacksConfig{
					Definitions: []StackDefinition{
						{Name: "stack_a", SwitchScript: "switch-a.sh"},
					},
					Default: "undefined_stack",
				},
			},
			wantErrors: 1,
			errorField: "not defined",
		},
		{
			name: "valid config with default stack",
			cfg: &Config{
				Version:   1,
				ConfigDir: tmpDir,
				Command:   CommandConfig{raw: "echo test"},
				Plugins:   PluginsConfig{Naming: &PluginConfig{Path: scriptA}},
				Inputs:    InputsConfig{Patterns: []PatternConfig{{Pattern: "*.txt"}}},
				Concurrency: ConcurrencyConfig{
					Mode: ConcurrencyStack,
				},
				Stacks: StacksConfig{
					Definitions: []StackDefinition{
						{Name: "stack_a", SwitchScript: "switch-a.sh"},
					},
					Default: "stack_a",
				},
			},
			wantErrors: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateStackMode(tt.cfg)
			if len(errs) != tt.wantErrors {
				t.Errorf("validateStackMode() returned %d errors, want %d", len(errs), tt.wantErrors)
				for i, err := range errs {
					t.Logf("  error %d: %v", i, err)
				}
			}
			if tt.wantErrors > 0 && len(errs) > 0 && tt.errorField != "" {
				found := false
				for _, err := range errs {
					if contains(err.Error(), tt.errorField) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error containing %q, got: %v", tt.errorField, errs)
				}
			}
		})
	}
}

func TestGetStackDefinition(t *testing.T) {
	cfg := &Config{
		Stacks: StacksConfig{
			Definitions: []StackDefinition{
				{Name: "stack_a", SwitchScript: "switch-a.sh"},
				{Name: "stack_b", SwitchScript: "switch-b.sh"},
			},
		},
	}

	tests := []struct {
		name      string
		stackName string
		wantNil   bool
		wantName  string
	}{
		{
			name:      "find existing stack",
			stackName: "stack_a",
			wantNil:   false,
			wantName:  "stack_a",
		},
		{
			name:      "find second stack",
			stackName: "stack_b",
			wantNil:   false,
			wantName:  "stack_b",
		},
		{
			name:      "stack not found",
			stackName: "nonexistent",
			wantNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := cfg.GetStackDefinition(tt.stackName)
			if tt.wantNil {
				if def != nil {
					t.Errorf("GetStackDefinition(%q) = %v, want nil", tt.stackName, def)
				}
			} else {
				if def == nil {
					t.Errorf("GetStackDefinition(%q) = nil, want %q", tt.stackName, tt.wantName)
				} else if def.Name != tt.wantName {
					t.Errorf("GetStackDefinition(%q).Name = %q, want %q", tt.stackName, def.Name, tt.wantName)
				}
			}
		})
	}
}

func TestHasStack(t *testing.T) {
	cfg := &Config{
		Stacks: StacksConfig{
			Definitions: []StackDefinition{
				{Name: "stack_a", SwitchScript: "switch-a.sh"},
			},
		},
	}

	if !cfg.HasStack("stack_a") {
		t.Error("HasStack(stack_a) = false, want true")
	}
	if cfg.HasStack("nonexistent") {
		t.Error("HasStack(nonexistent) = true, want false")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && searchSubstring(s, substr)))
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
