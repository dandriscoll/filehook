package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dandriscoll/filehook/internal/config"
	"github.com/fsnotify/fsnotify"
)

// Event represents a file event
type Event struct {
	Path         string
	IsModify     bool // true if modification, false if new file
	DiscoveredAt time.Time
	Pattern      *config.PatternConfig // the pattern that matched this file
}

// Watcher watches for file changes
type Watcher struct {
	fsWatcher *fsnotify.Watcher
	matcher   *Matcher
	debounce  time.Duration
	events    chan Event
	errors    chan error

	// Debounce state
	mu       sync.Mutex
	pending  map[string]*pendingEvent
	seenOnce map[string]bool // Track files we've seen to distinguish new vs modify

	// Close handling
	closeOnce sync.Once
	closeErr  error
}

type pendingEvent struct {
	event    Event
	timer    *time.Timer
	isModify bool
}

// New creates a new watcher
func New(matcher *Matcher, debounceDuration time.Duration) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher: %w", err)
	}

	return &Watcher{
		fsWatcher: fsw,
		matcher:   matcher,
		debounce:  debounceDuration,
		events:    make(chan Event, 100),
		errors:    make(chan error, 10),
		pending:   make(map[string]*pendingEvent),
		seenOnce:  make(map[string]bool),
	}, nil
}

// AddPath adds a path to watch recursively
func (w *Watcher) AddPath(path string) error {
	return filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			// Skip ignored directories
			if w.matcher.ShouldIgnore(p) {
				return filepath.SkipDir
			}

			if err := w.fsWatcher.Add(p); err != nil {
				return fmt.Errorf("failed to watch %s: %w", p, err)
			}
		}

		return nil
	})
}

// Events returns the channel of debounced events
func (w *Watcher) Events() <-chan Event {
	return w.events
}

// Errors returns the channel of errors
func (w *Watcher) Errors() <-chan error {
	return w.errors
}

// Start begins watching for events
func (w *Watcher) Start(ctx context.Context) {
	go w.run(ctx)
}

func (w *Watcher) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return
			}
			w.handleFSEvent(event)

		case err, ok := <-w.fsWatcher.Errors:
			if !ok {
				return
			}
			select {
			case w.errors <- err:
			default:
			}
		}
	}
}

func (w *Watcher) handleFSEvent(event fsnotify.Event) {
	path := event.Name

	// Check if this is a directory
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		// New directory - add it to watch
		if event.Has(fsnotify.Create) {
			if !w.matcher.ShouldIgnore(path) {
				w.fsWatcher.Add(path)
			}
		}
		return
	}

	// Skip if not a matching file
	result := w.matcher.MatchWithPattern(path)
	if !result.Matched {
		return
	}

	// Determine if this is a new file or modification
	isModify := false
	if event.Has(fsnotify.Write) || event.Has(fsnotify.Chmod) {
		w.mu.Lock()
		isModify = w.seenOnce[path]
		w.mu.Unlock()
	}

	// Handle create/write/chmod events (chmod covers touch updating mtime)
	if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) || event.Has(fsnotify.Chmod) {
		w.debounceEvent(path, isModify, result.Pattern)
	}
}

func (w *Watcher) debounceEvent(path string, isModify bool, pattern *config.PatternConfig) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Mark as seen
	w.seenOnce[path] = true

	// Cancel existing timer if any
	if p, exists := w.pending[path]; exists {
		p.timer.Stop()
		// Keep the original isModify state (first event determines)
		isModify = p.isModify
	}

	// Create new pending event
	pe := &pendingEvent{
		event: Event{
			Path:         path,
			IsModify:     isModify,
			DiscoveredAt: time.Now(),
			Pattern:      pattern,
		},
		isModify: isModify,
	}

	pe.timer = time.AfterFunc(w.debounce, func() {
		w.mu.Lock()
		delete(w.pending, path)
		w.mu.Unlock()

		select {
		case w.events <- pe.event:
		default:
			// Channel full, drop event
		}
	})

	w.pending[path] = pe
}

// Close closes the watcher and its event channel
func (w *Watcher) Close() error {
	w.closeOnce.Do(func() {
		w.closeErr = w.fsWatcher.Close()
		// Close the events channel to signal consumers to stop
		close(w.events)
	})
	return w.closeErr
}

// ScanExisting scans existing files and emits events for matches
func (w *Watcher) ScanExisting(ctx context.Context, paths []string) error {
	for _, watchPath := range paths {
		err := filepath.Walk(watchPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if info.IsDir() {
				if w.matcher.ShouldIgnore(path) {
					return filepath.SkipDir
				}
				return nil
			}

			result := w.matcher.MatchWithPattern(path)
			if result.Matched {
				w.mu.Lock()
				seen := w.seenOnce[path]
				w.seenOnce[path] = true
				w.mu.Unlock()

				event := Event{
					Path:         path,
					IsModify:     seen, // First scan = not a modification
					DiscoveredAt: time.Now(),
					Pattern:      result.Pattern,
				}

				select {
				case w.events <- event:
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			return nil
		})
		if err != nil {
			return err
		}
	}

	return nil
}
