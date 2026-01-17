package watcher

import (
	"testing"

	"github.com/dandriscoll/filehook/internal/config"
)

func TestMatcher_Matches(t *testing.T) {
	tests := []struct {
		name     string
		patterns []config.PatternConfig
		ignore   []string
		path     string
		want     bool
	}{
		{
			name: "simple pattern matches",
			patterns: []config.PatternConfig{
				{Pattern: "*.txt"},
			},
			path: "foo.txt",
			want: true,
		},
		{
			name: "simple pattern does not match",
			patterns: []config.PatternConfig{
				{Pattern: "*.txt"},
			},
			path: "foo.md",
			want: false,
		},
		{
			name: "global ignore excludes file",
			patterns: []config.PatternConfig{
				{Pattern: "*.txt"},
			},
			ignore: []string{"*.tmp"},
			path:   "foo.tmp",
			want:   false,
		},
		{
			name: "pattern with exclusion - file matches but is excluded",
			patterns: []config.PatternConfig{
				{Pattern: "*.txt", Exclude: []string{"*.bar.txt"}},
			},
			path: "foo.bar.txt",
			want: false,
		},
		{
			name: "pattern with exclusion - file matches and not excluded",
			patterns: []config.PatternConfig{
				{Pattern: "*.txt", Exclude: []string{"*.bar.txt"}},
			},
			path: "foo.txt",
			want: true,
		},
		{
			name: "multiple patterns - first excludes, second matches",
			patterns: []config.PatternConfig{
				{Pattern: "*.txt", Exclude: []string{"*.bar.txt"}},
				{Pattern: "*.bar.txt"},
			},
			path: "foo.bar.txt",
			want: true,
		},
		{
			name: "multiple patterns - first matches without exclusion",
			patterns: []config.PatternConfig{
				{Pattern: "*.txt", Exclude: []string{"*.bar.txt"}},
				{Pattern: "*.bar.txt"},
			},
			path: "hello.txt",
			want: true,
		},
		{
			name: "pattern with multiple exclusions",
			patterns: []config.PatternConfig{
				{Pattern: "*.txt", Exclude: []string{"*.bar.txt", "*.baz.txt"}},
			},
			path: "foo.baz.txt",
			want: false,
		},
		{
			name: "pattern with exclusion - different extension not excluded",
			patterns: []config.PatternConfig{
				{Pattern: "*.txt", Exclude: []string{"*.bar.txt"}},
			},
			path: "foo.baz.txt",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMatcher(tt.patterns, tt.ignore)
			got := m.Matches(tt.path)
			if got != tt.want {
				t.Errorf("Matches(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
