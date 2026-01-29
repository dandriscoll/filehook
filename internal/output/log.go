package output

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mattn/go-isatty"
)

// ANSI color codes
const (
	Reset   = "\033[0m"
	Bold    = "\033[1m"
	Dim     = "\033[2m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
)

// Logger provides colorized, structured log output for filehook runtime commands.
type Logger struct {
	w     io.Writer
	mu    sync.Mutex
	color bool
}

// NewLogger creates a logger that writes to stdout with color auto-detection.
func NewLogger() *Logger {
	color := isatty.IsTerminal(os.Stdout.Fd()) && os.Getenv("NO_COLOR") == ""
	return &Logger{w: os.Stdout, color: color}
}

// NewSilentLogger creates a logger that discards all output (for tests).
func NewSilentLogger() *Logger {
	return &Logger{w: io.Discard, color: false}
}

func (l *Logger) c(code, s string) string {
	if !l.color {
		return s
	}
	return code + s + Reset
}

func (l *Logger) printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ts := time.Now().Format("15:04:05")
	fmt.Fprintf(l.w, l.c(Dim, ts)+" "+format+"\n", args...)
}

// Banner prints a startup banner line (bold).
func (l *Logger) Banner(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintln(l.w, l.c(Bold, msg))
}

// Info prints a general informational message.
func (l *Logger) Info(format string, args ...any) {
	l.printf(format, args...)
}

// Queued prints a queued-file message with the filename highlighted.
func (l *Logger) Queued(inputPath string, extra string) {
	base := filepath.Base(inputPath)
	if extra != "" {
		l.printf("%s %s %s", l.c(Cyan, "+"), l.c(Bold, base), l.c(Dim, extra))
	} else {
		l.printf("%s %s", l.c(Cyan, "+"), l.c(Bold, base))
	}
}

// Processing prints a "processing" message.
func (l *Logger) Processing(inputPath string) {
	base := filepath.Base(inputPath)
	l.printf("%s %s", l.c(Yellow, "▶"), base)
}

// Completed prints a completion message with duration.
func (l *Logger) Completed(inputPath string, durationMs int64) {
	base := filepath.Base(inputPath)
	dur := formatMs(durationMs)
	l.printf("%s %s %s", l.c(Green, "✓"), base, l.c(Dim, dur))
}

// Failed prints a failure message.
func (l *Logger) Failed(inputPath string, exitCode int) {
	base := filepath.Base(inputPath)
	l.printf("%s %s %s", l.c(Red, "✗"), base, l.c(Red, fmt.Sprintf("exit %d", exitCode)))
}

// Skipped prints a skip message.
func (l *Logger) Skipped(inputPath string, reason string) {
	base := filepath.Base(inputPath)
	l.printf("%s %s %s", l.c(Dim, "–"), l.c(Dim, base), l.c(Dim, reason))
}

// StackSwitch prints a stack switch message.
func (l *Logger) StackSwitch(stackName string, durationMs int64) {
	dur := formatMs(durationMs)
	l.printf("%s switched to %s %s", l.c(Magenta, "⇄"), l.c(Bold, stackName), l.c(Dim, dur))
}

// StackSwitching prints a "switching" start message.
func (l *Logger) StackSwitching(stackName string) {
	l.printf("%s switching to %s…", l.c(Magenta, "⇄"), l.c(Bold, stackName))
}

// Warn prints a warning.
func (l *Logger) Warn(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.printf("%s %s", l.c(Yellow, "⚠"), msg)
}

// Error prints an error.
func (l *Logger) Error(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.printf("%s %s", l.c(Red, "✗"), msg)
}
