package watcher

import (
	"path/filepath"
	"strings"

	"github.com/dandriscoll/filehook/internal/config"
)

// Matcher handles pattern matching for input files
type Matcher struct {
	patterns      []config.PatternConfig
	ignore        []string
	patternFilter string // if non-empty, only match patterns with this name
}

// MatchResult contains information about a pattern match
type MatchResult struct {
	Matched bool
	Pattern *config.PatternConfig
}

// NewMatcher creates a new file matcher
func NewMatcher(patterns []config.PatternConfig, ignore []string) *Matcher {
	return &Matcher{
		patterns:      patterns,
		ignore:        ignore,
		patternFilter: "",
	}
}

// NewMatcherWithFilter creates a new file matcher that only matches patterns with the given name
func NewMatcherWithFilter(patterns []config.PatternConfig, ignore []string, patternFilter string) *Matcher {
	return &Matcher{
		patterns:      patterns,
		ignore:        ignore,
		patternFilter: patternFilter,
	}
}

// Matches returns true if the path matches the input patterns
// and doesn't match any ignore patterns (global or per-pattern)
func (m *Matcher) Matches(path string) bool {
	return m.MatchWithPattern(path).Matched
}

// MatchWithPattern returns the matching pattern info, or a non-match result
func (m *Matcher) MatchWithPattern(path string) *MatchResult {
	// Check global ignore patterns first
	if m.matchesAny(path, m.ignore) {
		return &MatchResult{Matched: false, Pattern: nil}
	}

	// Check each input pattern with its exclusions
	for i := range m.patterns {
		p := &m.patterns[i]

		// If a filter is active, only match patterns with that name
		if m.patternFilter != "" {
			if p.Name != m.patternFilter {
				continue // Skip patterns that don't match the filter
			}
		}

		if m.matchesPattern(path, p.Pattern) {
			// Check pattern-specific exclusions
			if len(p.Exclude) > 0 && m.matchesAny(path, p.Exclude) {
				continue // Try next pattern
			}
			return &MatchResult{Matched: true, Pattern: p}
		}
	}
	return &MatchResult{Matched: false, Pattern: nil}
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
