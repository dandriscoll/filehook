package watcher

import (
	"path/filepath"
	"strings"

	"github.com/dandriscoll/filehook/internal/config"
)

// Matcher handles pattern matching for input files
type Matcher struct {
	patterns []config.PatternConfig
	ignore   []string
}

// NewMatcher creates a new file matcher
func NewMatcher(patterns []config.PatternConfig, ignore []string) *Matcher {
	return &Matcher{
		patterns: patterns,
		ignore:   ignore,
	}
}

// Matches returns true if the path matches the input patterns
// and doesn't match any ignore patterns (global or per-pattern)
func (m *Matcher) Matches(path string) bool {
	// Check global ignore patterns first
	if m.matchesAny(path, m.ignore) {
		return false
	}

	// Check each input pattern with its exclusions
	for _, p := range m.patterns {
		if m.matchesPattern(path, p.Pattern) {
			// Check pattern-specific exclusions
			if len(p.Exclude) > 0 && m.matchesAny(path, p.Exclude) {
				continue // Try next pattern
			}
			return true
		}
	}
	return false
}

// matchesAny returns true if path matches any of the patterns
func (m *Matcher) matchesAny(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if m.matchesPattern(path, pattern) {
			return true
		}
	}
	return false
}

// matchesPattern returns true if path matches the given pattern
func (m *Matcher) matchesPattern(path string, pattern string) bool {
	base := filepath.Base(path)

	// Try matching against base name first (for simple patterns like "*.pdf")
	if matched, _ := filepath.Match(pattern, base); matched {
		return true
	}

	// Try matching against full path (for patterns with path separators)
	if strings.Contains(pattern, "/") || strings.Contains(pattern, string(filepath.Separator)) {
		if matched, _ := filepath.Match(pattern, path); matched {
			return true
		}
	}

	// Handle glob patterns like "**/*.txt"
	if strings.Contains(pattern, "**") {
		if m.matchGlob(path, pattern) {
			return true
		}
	}

	return false
}

// matchGlob handles ** glob patterns
func (m *Matcher) matchGlob(path, pattern string) bool {
	// Simple ** handling: match any number of path components
	parts := strings.Split(pattern, "**")
	if len(parts) != 2 {
		return false
	}

	prefix := strings.TrimSuffix(parts[0], "/")
	suffix := strings.TrimPrefix(parts[1], "/")

	if prefix != "" && !strings.HasPrefix(path, prefix) {
		return false
	}

	if suffix != "" {
		// Match the suffix against the base name or full remaining path
		if matched, _ := filepath.Match(suffix, filepath.Base(path)); matched {
			return true
		}
	}

	return suffix == ""
}

// ShouldIgnore returns true if the path should be ignored
func (m *Matcher) ShouldIgnore(path string) bool {
	return m.matchesAny(path, m.ignore)
}
