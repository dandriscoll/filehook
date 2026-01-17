# filehook

A CLI-first file transformation watcher. filehook watches for input files and runs configured commands to transform them into output files.

## Features

- **Config discovery**: Walks up from current directory to find `filehook.yaml`
- **File watching**: Monitors directories for new/modified files with debounce
- **Durable queue**: SQLite-backed job queue survives restarts
- **Plugin system**: External scripts for filename generation and processing logic
- **Parallel processing**: Configurable worker pool for concurrent jobs
- **Sequential scheduler**: Round-robin through file groups for ordered processing
- **CLI management**: Commands to inspect queue, view errors, retry failed jobs

## Installation

```bash
go install github.com/dandriscoll/filehook/cmd/filehook@latest
```

Or build from source:

```bash
git clone https://github.com/dandriscoll/filehook
cd filehook
go build -o filehook ./cmd/filehook
```

## Quickstart

### 1. Create a project directory

```bash
mkdir my-project
cd my-project
mkdir -p input output plugins
```

### 2. Create a naming plugin

The naming plugin handles both filename generation AND ready checks in a single JSON response (they're closely related since both depend on external source state).

```bash
cat > plugins/naming.sh << 'EOF'
#!/bin/bash
# Naming plugin - returns both outputs and ready status

INPUT_PATH="$1"
BASENAME=$(basename "$INPUT_PATH")
NAME="${BASENAME%.*}"
EXT="${BASENAME##*.}"

# Example: input.txt -> input.processed.txt
# Return JSON with outputs and ready status
echo "{\"outputs\": [\"${NAME}.processed.${EXT}\"], \"ready\": true}"
EOF
chmod +x plugins/naming.sh
```

### 3. Create a config file

```yaml
# filehook.yaml
version: 1

watch:
  paths:
    - ./input
  ignore:
    - "*.tmp"
  debounce_ms: 200

inputs:
  patterns:
    - "*.txt"

outputs:
  root: ./output

# Command with variable substitution
command: ["cp", "{{input}}", "{{output}}"]

plugins:
  naming:
    path: ./plugins/naming.sh

concurrency:
  mode: parallel
  max_workers: 4

on_modified: if-newer

state_dir: .filehook
```

### 4. Run filehook

One-shot mode (scan and process all files):
```bash
filehook run
```

Watch mode (continuous monitoring):
```bash
filehook watch
```

## CLI Commands

| Command | Description |
|---------|-------------|
| `filehook watch` | Start watcher and workers |
| `filehook run` | One-shot scan and process until queue empty |
| `filehook status` | Show queue length, active workers, last errors |
| `filehook queue list` | List queued items |
| `filehook errors list` | List failures with ids and summaries |
| `filehook errors show <id>` | Show details and logs for a failed job |
| `filehook retry <id\|all>` | Re-enqueue failed jobs |
| `filehook config show` | Show resolved config and discovered path |

### Global Flags

- `--config, -c`: Specify config file path (default: search for `filehook.yaml`)
- `--json`: Output in JSON format

## Configuration Reference

```yaml
version: 1                    # Config version (required, must be 1)

watch:
  paths:                      # Directories to watch (relative to config)
    - ./input
  ignore:                     # Patterns to ignore
    - "*.tmp"
    - ".git"
  debounce_ms: 200            # Debounce delay in milliseconds

inputs:
  patterns:                   # File patterns to process
    - "*.txt"
    - "*.md"

outputs:
  root: ./output              # Output directory root

command: ["cmd", "{{input}}", "{{output}}"]  # Command to run
# OR as a string (runs via sh -c):
# command: "cp {{input}} {{output}}"

plugins:
  naming:                     # Required: handles filename generation AND ready checks
    path: ./plugins/naming.sh
    args: []
  should_process:             # Optional: custom processing policy (NOT readiness - use naming for that)
    path: ./plugins/check.sh
    args: []
  group_key:                  # Optional: grouping for sequential scheduler
    path: ./plugins/group.sh
    args: []

concurrency:
  mode: parallel              # parallel | sequential_switch
  max_workers: 4              # Number of concurrent workers

on_modified: if-newer         # ignore | reprocess | if-newer | versioned

state_dir: .filehook          # Where to store queue database
```

### Command Variables

| Variable | Description |
|----------|-------------|
| `{{input}}` | Absolute input file path |
| `{{output}}` | First output path |
| `{{outputs}}` | Space-separated list of all output paths |
| `{{outputs_json}}` | JSON array of output paths |
| `{{output_dir}}` | Directory of first output |

### Environment Variables

The command also receives these environment variables:

- `FILEHOOK_INPUT`: Input file path
- `FILEHOOK_OUTPUT`: Colon-separated list of output paths
- `FILEHOOK_OUTPUT_DIR`: Output directory
- `FILEHOOK_JOB_ID`: Unique job identifier

### Modification Policies

| Policy | Behavior |
|--------|----------|
| `ignore` | Don't reprocess modified files |
| `reprocess` | Always reprocess on modification |
| `if-newer` | Reprocess if input is newer than outputs |
| `versioned` | Create versioned outputs (file.v1.txt, file.v2.txt, ...) |

## Plugin Contracts

### Naming Plugin (Required)

Handles both filename generation AND ready checks in a single call. These are combined because they're closely related - both depend on understanding the external source state.

**Invocation**: Called with input path as first argument, plus any configured args.

**Output**: JSON to stdout with:
- `"outputs"`: array of output filenames (required)
- `"ready"`: boolean indicating if file is ready for processing (optional, defaults to true)

```json
{
  "outputs": ["output1.txt", "output2.txt"],
  "ready": true
}
```

Paths are relative to `outputs.root` unless absolute. If `ready` is false, the file is skipped.

**Exit codes**:
- 0: Success
- Non-zero: Error (job marked failed)

**Example**:
```bash
#!/bin/bash
INPUT_PATH="$1"
BASENAME=$(basename "$INPUT_PATH")
NAME="${BASENAME%.*}"

# Check if file is ready (e.g., external system done writing)
READY=true

# Return both outputs and ready status
echo "{\"outputs\": [\"${NAME}.processed.txt\"], \"ready\": $READY}"
```

### Should-Process Plugin (Optional)

Custom logic to decide whether to process a file based on modification policy.

**IMPORTANT**: This is NOT for checking file readiness. Use the naming plugin's ready check for that. Should-process checks whether outputs need regeneration based on timestamps and policy.

**Invocation**: Receives JSON via stdin.

**Input**:
```json
{
  "input": "/path/to/input.txt",
  "outputs": ["/path/to/output.txt"],
  "input_mtime": "2024-01-15T10:30:00Z",
  "output_mtimes": {
    "/path/to/output.txt": "2024-01-15T09:00:00Z"
  }
}
```

Output mtimes are `null` if the file doesn't exist.

**Output**:
```json
{
  "process": true,
  "reason": "output missing"
}
```

### Group Key Plugin (Optional)

Generates a group key for the sequential switching scheduler.

**Invocation**: Called with input path as first argument.

**Output**: Single line with the group key string.

**Default grouping**: First subdirectory of the input path.

## Troubleshooting

### Config not found

filehook searches upward from the current directory for `filehook.yaml` or `filehook.yml`. Specify the path explicitly with `--config`:

```bash
filehook --config /path/to/filehook.yaml run
```

### Plugin permission denied

Ensure plugins are executable:

```bash
chmod +x ./plugins/*.sh
```

### Jobs not processing

Check if the file matches input patterns:

```bash
filehook config show  # Shows resolved patterns
```

Check for pending/failed jobs:

```bash
filehook status
filehook errors list
```

### Stale running jobs

If filehook crashes, jobs may be left in "running" state. They are automatically reset to "pending" on the next startup.

### View job details

```bash
filehook errors show <job-id>
```

This shows stdout, stderr, exit code, and duration.

### Retry failed jobs

```bash
filehook retry <job-id>  # Retry specific job
filehook retry all       # Retry all failed jobs
```

## Development

### Running tests

```bash
./scripts/smoke_test.sh
```

### Project structure

```
filehook/
├── cmd/filehook/           # CLI entry point
├── internal/
│   ├── cli/                # Command implementations
│   ├── config/             # Config types and loading
│   ├── plugin/             # Plugin execution
│   ├── queue/              # SQLite job queue
│   ├── watcher/            # File watching
│   ├── worker/             # Job execution
│   └── output/             # Output formatting
├── examples/               # Example config and plugins
└── scripts/                # Test scripts
```

## License

MIT
