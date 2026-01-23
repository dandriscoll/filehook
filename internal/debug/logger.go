package debug

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger provides verbose debug logging to a file
type Logger struct {
	mu      sync.Mutex
	file    *os.File
	enabled bool
}

// New creates a new debug logger. If enabled is false, all logging is a no-op.
func New(stateDir string, enabled bool) (*Logger, error) {
	if !enabled {
		return &Logger{enabled: false}, nil
	}

	// Ensure state directory exists
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	logPath := filepath.Join(stateDir, "debug.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open debug log: %w", err)
	}

	logger := &Logger{
		file:    file,
		enabled: true,
	}

	logger.Log("=== Debug logging started ===")
	return logger, nil
}

// Close closes the debug log file
func (l *Logger) Close() error {
	if !l.enabled || l.file == nil {
		return nil
	}
	l.Log("=== Debug logging stopped ===")
	return l.file.Close()
}

// Enabled returns whether debug logging is enabled
func (l *Logger) Enabled() bool {
	return l.enabled
}

// Log writes a message to the debug log
func (l *Logger) Log(format string, args ...interface{}) {
	if !l.enabled {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(l.file, "[%s] %s\n", timestamp, msg)
}

// Section writes a section header to the debug log
func (l *Logger) Section(title string) {
	if !l.enabled {
		return
	}
	l.Log("--- %s ---", title)
}

// PluginCall logs a plugin invocation with its inputs and outputs
func (l *Logger) PluginCall(pluginName, operation string, args []string, stdin string, stdout string, stderr string, exitCode int, duration time.Duration) {
	if !l.enabled {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	fmt.Fprintf(l.file, "[%s] PLUGIN: %s %s\n", timestamp, pluginName, operation)
	fmt.Fprintf(l.file, "  Args: %v\n", args)
	if stdin != "" {
		fmt.Fprintf(l.file, "  Stdin: %s\n", truncate(stdin, 500))
	}
	fmt.Fprintf(l.file, "  Exit: %d (took %v)\n", exitCode, duration)
	if stdout != "" {
		fmt.Fprintf(l.file, "  Stdout: %s\n", truncate(stdout, 1000))
	}
	if stderr != "" {
		fmt.Fprintf(l.file, "  Stderr: %s\n", truncate(stderr, 1000))
	}
	fmt.Fprintln(l.file)
}

// Decision logs a decision point with reasoning
func (l *Logger) Decision(context string, decision string, reason string) {
	if !l.enabled {
		return
	}
	l.Log("DECISION [%s]: %s - %s", context, decision, reason)
}

// Event logs a file system event
func (l *Logger) Event(eventType string, path string, details string) {
	if !l.enabled {
		return
	}
	if details != "" {
		l.Log("EVENT: %s %s (%s)", eventType, path, details)
	} else {
		l.Log("EVENT: %s %s", eventType, path)
	}
}

// JobStart logs when a job starts execution
func (l *Logger) JobStart(jobID string, inputPath string, targetType string, command []string) {
	if !l.enabled {
		return
	}
	l.Log("JOB START: %s", jobID)
	l.Log("  Input: %s", inputPath)
	l.Log("  TargetType: %s", targetType)
	l.Log("  Command: %v", command)
}

// JobEnd logs when a job completes
func (l *Logger) JobEnd(jobID string, exitCode int, duration time.Duration, stdout string, stderr string, err error) {
	if !l.enabled {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	fmt.Fprintf(l.file, "[%s] JOB END: %s\n", timestamp, jobID)
	fmt.Fprintf(l.file, "  Exit: %d (took %v)\n", exitCode, duration)
	if err != nil {
		fmt.Fprintf(l.file, "  Error: %v\n", err)
	}
	if stdout != "" {
		fmt.Fprintf(l.file, "  Stdout:\n%s\n", indent(truncate(stdout, 2000), "    "))
	}
	if stderr != "" {
		fmt.Fprintf(l.file, "  Stderr:\n%s\n", indent(truncate(stderr, 2000), "    "))
	}
	fmt.Fprintln(l.file)
}

// Writer returns an io.Writer that writes to the debug log with a prefix
func (l *Logger) Writer(prefix string) io.Writer {
	if !l.enabled {
		return io.Discard
	}
	return &prefixWriter{logger: l, prefix: prefix}
}

type prefixWriter struct {
	logger *Logger
	prefix string
}

func (w *prefixWriter) Write(p []byte) (n int, err error) {
	w.logger.Log("%s: %s", w.prefix, string(p))
	return len(p), nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "... (truncated)"
}

func indent(s string, prefix string) string {
	if s == "" {
		return s
	}
	result := ""
	for i, line := range splitLines(s) {
		if i > 0 {
			result += "\n"
		}
		result += prefix + line
	}
	return result
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
