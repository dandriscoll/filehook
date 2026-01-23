package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dandriscoll/filehook/internal/queue"
)

// Formatter handles output formatting
type Formatter struct {
	json   bool
	writer io.Writer
}

// New creates a new formatter
func New(jsonMode bool) *Formatter {
	return &Formatter{
		json:   jsonMode,
		writer: os.Stdout,
	}
}

// PrintStats prints queue statistics
func (f *Formatter) PrintStats(stats *queue.QueueStats) error {
	if f.json {
		return f.printJSON(stats)
	}

	fmt.Fprintf(f.writer, "Queue Status:\n")
	fmt.Fprintf(f.writer, "  Pending:   %d\n", stats.Pending)
	fmt.Fprintf(f.writer, "  Running:   %d\n", stats.Running)
	fmt.Fprintf(f.writer, "  Completed: %d\n", stats.Completed)
	fmt.Fprintf(f.writer, "  Failed:    %d\n", stats.Failed)
	fmt.Fprintf(f.writer, "  Total:     %d\n", stats.Total)
	return nil
}

// PrintJobSummaries prints a list of job summaries
func (f *Formatter) PrintJobSummaries(jobs []queue.JobSummary, title string) error {
	if f.json {
		return f.printJSON(jobs)
	}

	if len(jobs) == 0 {
		fmt.Fprintf(f.writer, "%s: (none)\n", title)
		return nil
	}

	fmt.Fprintf(f.writer, "%s:\n", title)
	for _, job := range jobs {
		age := formatDuration(time.Since(job.CreatedAt))
		shortPath := shortenPath(job.InputPath, 50)
		line := fmt.Sprintf("  %s  %s  (%s ago)", job.ID[:8], shortPath, age)
		if job.Error != "" {
			errSummary := job.Error
			if len(errSummary) > 40 {
				errSummary = errSummary[:40] + "..."
			}
			line += fmt.Sprintf(" - %s", errSummary)
		}
		fmt.Fprintln(f.writer, line)
	}
	return nil
}

// PrintJob prints full job details
func (f *Formatter) PrintJob(job *queue.Job) error {
	if f.json {
		return f.printJSON(job)
	}

	fmt.Fprintf(f.writer, "Job: %s\n", job.ID)
	fmt.Fprintf(f.writer, "  Input:    %s\n", job.InputPath)
	if len(job.OutputPaths) > 0 {
		fmt.Fprintf(f.writer, "  Outputs:  %s\n", strings.Join(job.OutputPaths, ", "))
	} else if job.TargetType != "" {
		fmt.Fprintf(f.writer, "  Target:   %s (outputs calculated at execution time)\n", job.TargetType)
	} else {
		fmt.Fprintf(f.writer, "  Outputs:  (calculated at execution time)\n")
	}
	fmt.Fprintf(f.writer, "  Status:   %s\n", job.Status)
	if job.GroupKey != "" {
		fmt.Fprintf(f.writer, "  Group:    %s\n", job.GroupKey)
	}
	fmt.Fprintf(f.writer, "  Created:  %s\n", job.CreatedAt.Format(time.RFC3339))
	if job.StartedAt != nil {
		fmt.Fprintf(f.writer, "  Started:  %s\n", job.StartedAt.Format(time.RFC3339))
	}
	if job.CompletedAt != nil {
		fmt.Fprintf(f.writer, "  Completed: %s\n", job.CompletedAt.Format(time.RFC3339))
	}
	if job.ExitCode != nil {
		fmt.Fprintf(f.writer, "  Exit code: %d\n", *job.ExitCode)
	}
	if job.DurationMs != nil {
		fmt.Fprintf(f.writer, "  Duration: %dms\n", *job.DurationMs)
	}
	if job.Error != "" {
		fmt.Fprintf(f.writer, "  Error:    %s\n", job.Error)
	}
	if job.Stdout != "" {
		fmt.Fprintf(f.writer, "\n--- stdout ---\n%s\n", job.Stdout)
	}
	if job.Stderr != "" {
		fmt.Fprintf(f.writer, "\n--- stderr ---\n%s\n", job.Stderr)
	}

	return nil
}

// PrintMessage prints a simple message
func (f *Formatter) PrintMessage(message string) error {
	if f.json {
		return f.printJSON(map[string]string{"message": message})
	}
	fmt.Fprintln(f.writer, message)
	return nil
}

// PrintError prints an error message
func (f *Formatter) PrintError(err error) {
	if f.json {
		f.printJSON(map[string]string{"error": err.Error()})
		return
	}
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
}

func (f *Formatter) printJSON(v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(f.writer, string(data))
	return nil
}

// shortenPath shortens a path for display
func shortenPath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}

	// Try to keep the filename and shorten the directory
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	if len(base) >= maxLen-3 {
		return "..." + path[len(path)-maxLen+3:]
	}

	remaining := maxLen - len(base) - 4 // 4 for ".../"
	if remaining > 0 && len(dir) > remaining {
		return "..." + dir[len(dir)-remaining:] + "/" + base
	}

	return "..." + path[len(path)-maxLen+3:]
}

// formatDuration formats a duration for human display
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
