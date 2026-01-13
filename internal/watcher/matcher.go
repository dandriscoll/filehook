package watcher

import (
	"path/filepath"
	"strings"
)

// Matcher handles pattern matching for input files
type Matcher struct {
	patterns []string
	ignore   []string
}

// NewMatcher creates a new file matcher
func NewMatcher(patterns, ignore []string) *Matcher {
	return &Matcher{
		patterns: patterns,
		ignore:   ignore,
	}
}

// Matches returns true if the path matches the input patterns
// and doesn't match any ignore patterns
func (m *Matcher) Matches(path string) bool {
	// Check ignore patterns first
	if m.matchesAny(path, m.ignore) {
		return false
	}

	// Check if it matches any input pattern
	return m.matchesAny(path, m.patterns)
}

// matchesAny returns true if path matches any of the patterns
func (m *Matcher) matchesAny(path string, patterns []string) bool {
	base := filepath.Base(path)

	for _, pattern := range patterns {
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
