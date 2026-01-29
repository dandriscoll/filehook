package cli

import (
	"fmt"
	"os"
)

var showDescriptor bool

// Boutiques descriptor schema: https://github.com/boutiques/boutiques/blob/master/boutiques/schema/descriptor.schema.json
// Boutiques invocation docs: https://boutiques.github.io/doc/_invocation.html
// Boutiques general docs: https://boutiques.github.io/
const boutiquesDescriptor = `{
  "name": "filehook",
  "tool-version": "1.0.0",
  "schema-version": "0.5",
  "x-spec-url": "https://github.com/boutiques/boutiques/blob/master/boutiques/schema/descriptor.schema.json",
  "description": "A CLI file transformation watcher. filehook watches for input files and runs configured commands to transform them into output files. It supports plugins for filename generation and processing decisions, parallel or sequential job execution, and durable on-disk queuing.",
  "command-line": "filehook [COMMAND] [CONFIG] [DIRECTORY] [JSON_FLAG] [DRY_RUN] [DEBUG]",
  "inputs": [
    {
      "id": "command",
      "name": "Command",
      "type": "String",
      "description": "The subcommand to execute: watch, run, status, state, queue, errors, retry, config, init",
      "optional": true,
      "value-key": "[COMMAND]",
      "value-choices": ["watch", "run", "status", "state", "queue", "errors", "retry", "config", "init"]
    },
    {
      "id": "config",
      "name": "Config file",
      "type": "File",
      "description": "Path to config file (default: search for filehook.yaml)",
      "optional": true,
      "command-line-flag": "--config",
      "value-key": "[CONFIG]"
    },
    {
      "id": "directory",
      "name": "Target directory",
      "type": "String",
      "description": "Target directory (finds config upward, processes only this dir)",
      "optional": true,
      "command-line-flag": "--directory",
      "value-key": "[DIRECTORY]"
    },
    {
      "id": "json_output",
      "name": "JSON output",
      "type": "Flag",
      "description": "Output in JSON format",
      "optional": true,
      "command-line-flag": "--json",
      "value-key": "[JSON_FLAG]"
    },
    {
      "id": "dry_run",
      "name": "Dry run",
      "type": "Flag",
      "description": "Print what would be done without executing",
      "optional": true,
      "command-line-flag": "--dry-run",
      "value-key": "[DRY_RUN]"
    },
    {
      "id": "debug",
      "name": "Debug mode",
      "type": "Flag",
      "description": "Enable verbose debug logging to .filehook/debug.log",
      "optional": true,
      "command-line-flag": "--debug",
      "value-key": "[DEBUG]"
    }
  ],
  "output-files": [
    {
      "id": "state_directory",
      "name": "State directory",
      "description": "Directory containing queue database and debug logs",
      "path-template": ".filehook/",
      "optional": true
    },
    {
      "id": "queue_database",
      "name": "Queue database",
      "description": "SQLite database storing job queue state",
      "path-template": ".filehook/queue.db",
      "optional": true
    },
    {
      "id": "debug_log",
      "name": "Debug log",
      "description": "Debug log file (when --debug is enabled)",
      "path-template": ".filehook/debug.log",
      "optional": true
    }
  ],
  "groups": [
    {
      "id": "subcommands",
      "name": "Subcommands",
      "description": "Available subcommands for filehook",
      "members": ["command"]
    },
    {
      "id": "global_options",
      "name": "Global options",
      "description": "Options available for all subcommands",
      "members": ["config", "directory", "json_output", "dry_run", "debug"]
    }
  ],
  "custom": {
    "subcommands": {
      "watch": {
        "description": "Start watcher and workers - continuously monitors for input files",
        "options": ["--pattern"]
      },
      "run": {
        "description": "One-shot scan and process until queue empty",
        "options": ["--run-one", "--pattern"]
      },
      "status": {
        "description": "Show queue length, active workers, and last errors",
        "options": ["--stacks"]
      },
      "state": {
        "description": "Show process state (for programmatic use): not_running, idle, or processing"
      },
      "queue": {
        "description": "Queue management commands",
        "subcommands": {
          "list": "List queued items ordered by priority",
          "move": "Move a job in the queue priority (top, up, down, bottom)",
          "priority": "Set the absolute priority of a job"
        }
      },
      "errors": {
        "description": "Error management commands",
        "subcommands": {
          "list": "List failed jobs",
          "show": "Show details of a failed job"
        }
      },
      "retry": {
        "description": "Re-enqueue failed jobs (by ID or 'all')"
      },
      "config": {
        "description": "Config management commands",
        "subcommands": {
          "show": "Show resolved configuration"
        }
      },
      "init": {
        "description": "Initialize a new filehook.yaml config file"
      }
    }
  }
}`

func init() {
	rootCmd.PersistentFlags().BoolVar(&showDescriptor, "descriptor", false, "print Boutiques descriptor JSON and exit")
}

// checkDescriptorFlag checks if --descriptor was passed and handles it.
// This should be called early in Execute before running the command.
func checkDescriptorFlag() {
	if showDescriptor {
		fmt.Println(boutiquesDescriptor)
		os.Exit(0)
	}
}

// GetBoutiquesDescriptor returns the Boutiques descriptor JSON string.
// This is exposed for testing purposes.
func GetBoutiquesDescriptor() string {
	return boutiquesDescriptor
}
