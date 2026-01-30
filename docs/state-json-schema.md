# `filehook status --json` / `filehook state --json` Output Format

Machine-readable output from `filehook status -c <config> --json` or
`filehook state -c <config> --json`. Both commands produce the same
single JSON object on stdout (no surrounding text).

## Top-level object: `FullStatus`

| Field | Type | Required | Description |
|---|---|---|---|
| `state` | string | yes | One of `"not_running"`, `"idle"`, `"processing"` |
| `process` | ProcessInfo | no | Legacy single-process info (backward compat) |
| `scheduler` | ProcessInfo | no | Scheduler process (multi-process mode) |
| `producers` | ProcessInfo[] | no | Producer processes (multi-process mode) |
| `stats` | QueueStats | yes | Aggregate queue counts |
| `current_job` | JobSummary | no | Currently running job, if any |
| `next_job` | JobSummary | no | Next job to be dequeued, if any |
| `queue_length` | int | yes | Number of pending jobs |
| `recently_completed` | JobSummary[] | no | Last 5 completed jobs (newest first) |
| `recently_failed` | JobSummary[] | no | Last 5 failed jobs |
| `pending_jobs` | JobSummary[] | no | First 10 pending jobs (priority desc, then oldest first) |
| `current_stack` | string | no | Active stack name (stack mode only) |
| `pending_by_stack` | map[string]int | no | Pending job counts keyed by stack name (stack mode only). Empty string key `""` means no stack. |

## `ProcessInfo`

| Field | Type | Required | Description |
|---|---|---|---|
| `pid` | int | yes | OS process ID |
| `command` | string | yes | `"watch"`, `"run"`, or `"serve"` |
| `role` | string | yes | `"producer"`, `"scheduler"`, or `"legacy"` |
| `instance_id` | string | no | Unique instance identifier |
| `started_at` | string | yes | RFC 3339 timestamp |
| `heartbeat_at` | string | yes | RFC 3339 timestamp of last heartbeat |

## `QueueStats`

| Field | Type | Description |
|---|---|---|
| `pending` | int | Jobs waiting to run |
| `running` | int | Jobs currently executing |
| `completed` | int | Successfully finished jobs |
| `failed` | int | Jobs that errored or exited non-zero |
| `total` | int | Sum of all statuses |

## `JobSummary`

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | yes | UUID |
| `input_path` | string | yes | Absolute path of the input file |
| `status` | string | yes | `"pending"`, `"running"`, `"completed"`, or `"failed"` |
| `priority` | int | yes | Higher = dequeued first. Default `0`. |
| `group_key` | string | no | Group key for grouped execution |
| `stack_name` | string | no | Stack this job belongs to |
| `instance_id` | string | no | Instance that enqueued this job |
| `created_at` | string | yes | RFC 3339 timestamp |
| `completed_at` | string | no | RFC 3339 timestamp (present for completed/failed jobs) |
| `duration_ms` | int | no | Execution wall-clock time in milliseconds (present for completed/failed jobs) |
| `output_paths` | string[] | no | Output file paths produced by the job (present for completed/failed jobs that had outputs) |
| `error` | string | no | Error message (failed jobs only) |

## `state` field semantics

| Value | Meaning |
|---|---|
| `not_running` | No filehook process is alive |
| `idle` | Process is alive but nothing pending or running |
| `processing` | Process is alive with pending or running jobs |

## Example

```json
{
  "state": "processing",
  "scheduler": {
    "pid": 12345,
    "command": "serve",
    "role": "scheduler",
    "started_at": "2026-01-29T10:00:00Z",
    "heartbeat_at": "2026-01-29T10:05:00Z"
  },
  "producers": [
    {
      "pid": 12346,
      "command": "watch",
      "role": "producer",
      "instance_id": "abc123",
      "started_at": "2026-01-29T10:00:01Z",
      "heartbeat_at": "2026-01-29T10:05:01Z"
    }
  ],
  "stats": {
    "pending": 3,
    "running": 1,
    "completed": 42,
    "failed": 2,
    "total": 48
  },
  "current_job": {
    "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "input_path": "/data/incoming/file.csv",
    "status": "running",
    "priority": 0,
    "created_at": "2026-01-29T10:04:50Z"
  },
  "next_job": {
    "id": "b2c3d4e5-f6a7-8901-bcde-f12345678901",
    "input_path": "/data/incoming/file2.csv",
    "status": "pending",
    "priority": 5,
    "created_at": "2026-01-29T10:04:55Z"
  },
  "queue_length": 3,
  "recently_completed": [
    {
      "id": "c3d4e5f6-a7b8-9012-cdef-123456789012",
      "input_path": "/data/incoming/done.csv",
      "status": "completed",
      "priority": 0,
      "created_at": "2026-01-29T10:03:00Z",
      "completed_at": "2026-01-29T10:03:12Z",
      "duration_ms": 12450,
      "output_paths": ["/data/processed/done.parquet"]
    }
  ],
  "recently_failed": [
    {
      "id": "d4e5f6a7-b8c9-0123-defa-234567890123",
      "input_path": "/data/incoming/bad.csv",
      "status": "failed",
      "priority": 0,
      "created_at": "2026-01-29T10:02:00Z",
      "completed_at": "2026-01-29T10:02:05Z",
      "duration_ms": 5100,
      "error": "exit code 1"
    }
  ],
  "pending_jobs": [
    {
      "id": "b2c3d4e5-f6a7-8901-bcde-f12345678901",
      "input_path": "/data/incoming/file2.csv",
      "status": "pending",
      "priority": 5,
      "created_at": "2026-01-29T10:04:55Z"
    }
  ],
  "current_stack": "gpu",
  "pending_by_stack": {
    "gpu": 2,
    "cpu": 1
  }
}
```
