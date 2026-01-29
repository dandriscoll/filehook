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
		shortPath := shortenPath(job.InputPath, 50)
		var line string
		if job.CompletedAt != nil {
			age := FormatDuration(time.Since(*job.CompletedAt))
			dur := ""
			if job.DurationMs != nil {
				dur = fmt.Sprintf(" in %s", formatMs(*job.DurationMs))
			}
			out := ""
			if len(job.OutputPaths) > 0 {
				out = fmt.Sprintf(" -> %s", filepath.Base(job.OutputPaths[0]))
				if len(job.OutputPaths) > 1 {
					out += fmt.Sprintf(" (+%d)", len(job.OutputPaths)-1)
				}
			}
			line = fmt.Sprintf("  %s  %s%s%s  (%s ago)", job.ID[:8], shortPath, out, dur, age)
		} else {
			age := FormatDuration(time.Since(job.CreatedAt))
			line = fmt.Sprintf("  %s  %s  (%s ago)", job.ID[:8], shortPath, age)
		}
		if job.Priority != 0 {
			line += fmt.Sprintf(" [priority %d]", job.Priority)
		}
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

// formatMs formats milliseconds into a human-readable duration string
func formatMs(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	secs := float64(ms) / 1000.0
	if secs < 60 {
		return fmt.Sprintf("%.1fs", secs)
	}
	return fmt.Sprintf("%.0fm%.0fs", secs/60, float64(int(secs)%60))
}

// FormatDuration formats a duration for human display
func FormatDuration(d time.Duration) string {
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

// StackStatusData holds stack status for JSON output
type StackStatusData struct {
	Mode            string                `json:"mode"`
	CurrentStack    string                `json:"current_stack"`
	PendingByStack  map[string]int        `json:"pending_by_stack,omitempty"`
	StackStatistics []queue.StackStats    `json:"stack_statistics,omitempty"`
}

// PrintStackStatus prints stack status information
func (f *Formatter) PrintStackStatus(data *StackStatusData) error {
	if f.json {
		return f.printJSON(data)
	}
	// Non-JSON output is handled directly in the status command
	return nil
}

// PrintFullStatus prints the full process status (for the state command)
func (f *Formatter) PrintFullStatus(status *queue.FullStatus) error {
	if f.json {
		return f.printJSON(status)
	}

	// Simple human-readable output
	fmt.Fprintf(f.writer, "State: %s\n", status.State)

	if status.Scheduler != nil {
		fmt.Fprintf(f.writer, "Scheduler: PID %d (running since %s)\n",
			status.Scheduler.PID,
			status.Scheduler.StartedAt.Format(time.RFC3339))
	}

	if len(status.Producers) > 0 {
		fmt.Fprintf(f.writer, "Producers:\n")
		for _, p := range status.Producers {
			instance := p.InstanceID
			if instance == "" {
				instance = "(none)"
			}
			fmt.Fprintf(f.writer, "  PID %d: %s instance=%s (since %s)\n",
				p.PID, p.Command, instance, p.StartedAt.Format(time.RFC3339))
		}
	}

	if status.Process != nil && status.Scheduler == nil && len(status.Producers) == 0 {
		fmt.Fprintf(f.writer, "Process: %s (PID %d, running since %s)\n",
			status.Process.Command, status.Process.PID,
			status.Process.StartedAt.Format(time.RFC3339))
	}

	fmt.Fprintf(f.writer, "Queue: %d pending, %d running, %d completed, %d failed\n",
		status.Stats.Pending, status.Stats.Running, status.Stats.Completed, status.Stats.Failed)

	if status.CurrentJob != nil {
		fmt.Fprintf(f.writer, "Current: %s %s\n", status.CurrentJob.ID[:8], shortenPath(status.CurrentJob.InputPath, 50))
	}

	if status.NextJob != nil {
		priority := ""
		if status.NextJob.Priority != 0 {
			priority = fmt.Sprintf(" (priority %d)", status.NextJob.Priority)
		}
		fmt.Fprintf(f.writer, "Next: %s %s%s\n", status.NextJob.ID[:8], shortenPath(status.NextJob.InputPath, 50), priority)
	}

	if status.CurrentStack != "" {
		fmt.Fprintf(f.writer, "Stack: %s\n", status.CurrentStack)
	}

	if len(status.PendingByStack) > 0 {
		fmt.Fprintf(f.writer, "Pending by stack:")
		for stack, count := range status.PendingByStack {
			label := stack
			if label == "" {
				label = "(none)"
			}
			fmt.Fprintf(f.writer, " %s=%d", label, count)
		}
		fmt.Fprintln(f.writer)
	}

	if len(status.RecentlyCompleted) > 0 {
		fmt.Fprintln(f.writer)
		f.PrintJobSummaries(status.RecentlyCompleted, "Recently completed")
	}

	if len(status.RecentlyFailed) > 0 {
		fmt.Fprintln(f.writer)
		f.PrintJobSummaries(status.RecentlyFailed, "Recently failed")
	}

	return nil
}
