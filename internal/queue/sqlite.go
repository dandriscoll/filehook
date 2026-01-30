package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// SQLiteStore implements Store using SQLite
type SQLiteStore struct {
	db   *sql.DB
	path string
}

// NewSQLiteStore creates a new SQLite store at the given path
func NewSQLiteStore(stateDir string) (*SQLiteStore, error) {
	// Ensure state directory exists
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	dbPath := filepath.Join(stateDir, "queue.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(1) // SQLite doesn't support multiple writers
	db.SetMaxIdleConns(1)

	// Set PRAGMAs directly — DSN parameters are unreliable with modernc.org/sqlite
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set journal_mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=30000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set busy_timeout: %w", err)
	}

	return &SQLiteStore{
		db:   db,
		path: dbPath,
	}, nil
}

// Initialize creates the necessary tables
func (s *SQLiteStore) Initialize(ctx context.Context) error {
	schema := `
		CREATE TABLE IF NOT EXISTS jobs (
			id TEXT PRIMARY KEY,
			input_path TEXT NOT NULL,
			target_type TEXT,
			is_modify INTEGER DEFAULT 0,
			status TEXT NOT NULL,
			group_key TEXT,
			stack_name TEXT,
			command TEXT,
			created_at TEXT NOT NULL,
			started_at TEXT,
			completed_at TEXT,
			exit_code INTEGER,
			duration_ms INTEGER,
			error TEXT,
			stdout TEXT,
			stderr TEXT
		);

		CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
		CREATE INDEX IF NOT EXISTS idx_jobs_group_key ON jobs(group_key);
		CREATE INDEX IF NOT EXISTS idx_jobs_input_path ON jobs(input_path);
		CREATE INDEX IF NOT EXISTS idx_jobs_input_status ON jobs(input_path, status);
		CREATE INDEX IF NOT EXISTS idx_jobs_stack_status ON jobs(stack_name, status);

		-- Stack state table (singleton)
		CREATE TABLE IF NOT EXISTS stack_state (
			id INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
			current_stack TEXT,
			last_switch_at TEXT,
			last_switch_duration_ms INTEGER
		);

		-- Per-stack statistics
		CREATE TABLE IF NOT EXISTS stack_stats (
			stack_name TEXT PRIMARY KEY,
			job_count INTEGER DEFAULT 0,
			total_job_duration_ms INTEGER DEFAULT 0,
			switch_count INTEGER DEFAULT 0,
			total_switch_duration_ms INTEGER DEFAULT 0
		);

		-- Process registration (for detecting running instances)
		CREATE TABLE IF NOT EXISTS process_info (
			pid INTEGER PRIMARY KEY,
			command TEXT NOT NULL,
			started_at TEXT NOT NULL
		);
	`

	_, err := s.db.ExecContext(ctx, schema)
	if err != nil {
		return err
	}

	// Migration: add columns if they don't exist
	migrations := []string{
		"ALTER TABLE jobs ADD COLUMN target_type TEXT",
		"ALTER TABLE jobs ADD COLUMN is_modify INTEGER DEFAULT 0",
		"ALTER TABLE jobs ADD COLUMN stack_name TEXT",
		"ALTER TABLE jobs ADD COLUMN priority INTEGER DEFAULT 0",
		"ALTER TABLE jobs ADD COLUMN instance_id TEXT",
		"ALTER TABLE jobs ADD COLUMN claimed_by INTEGER",
		"ALTER TABLE jobs ADD COLUMN output_paths TEXT",
		"ALTER TABLE process_info ADD COLUMN role TEXT DEFAULT 'legacy'",
		"ALTER TABLE process_info ADD COLUMN instance_id TEXT",
		"ALTER TABLE process_info ADD COLUMN heartbeat_at TEXT",
	}

	for _, migration := range migrations {
		// Ignore errors - column may already exist
		s.db.ExecContext(ctx, migration)
	}

	// Create index for stack_name if it doesn't exist
	s.db.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_jobs_stack_status ON jobs(stack_name, status)")

	// Create index for priority ordering
	s.db.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_jobs_priority ON jobs(status, priority DESC, created_at ASC)")

	// Create index for instance filtering
	s.db.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_jobs_instance ON jobs(instance_id, status)")

	return nil
}

// Close closes the database connection
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// Enqueue adds a new job to the queue
func (s *SQLiteStore) Enqueue(ctx context.Context, job *Job) error {
	// Check for existing pending job — allow enqueuing even if a job is currently
	// running, so that edits made during processing are not lost
	exists, err := s.HasPending(ctx, job.InputPath)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("job for %q is already pending", job.InputPath)
	}

	if job.ID == "" {
		job.ID = uuid.New().String()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	if job.Status == "" {
		job.Status = JobStatusPending
	}

	var commandJSON *string
	if len(job.Command) > 0 {
		b, err := json.Marshal(job.Command)
		if err != nil {
			return fmt.Errorf("failed to marshal command: %w", err)
		}
		s := string(b)
		commandJSON = &s
	}

	var targetType *string
	if job.TargetType != "" {
		targetType = &job.TargetType
	}

	var stackName *string
	if job.StackName != "" {
		stackName = &job.StackName
	}

	var instanceID *string
	if job.InstanceID != "" {
		instanceID = &job.InstanceID
	}

	isModify := 0
	if job.IsModify {
		isModify = 1
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO jobs (id, input_path, target_type, is_modify, status, priority, group_key, stack_name, instance_id, command, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, job.ID, job.InputPath, targetType, isModify, job.Status, job.Priority, job.GroupKey, stackName, instanceID, commandJSON, job.CreatedAt.Format(time.RFC3339Nano))

	return err
}

// Dequeue gets the next pending job and marks it as running
func (s *SQLiteStore) Dequeue(ctx context.Context) (*Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Get highest priority pending job (priority DESC, then oldest first)
	row := tx.QueryRowContext(ctx, `
		SELECT id, input_path, target_type, is_modify, status, priority, group_key, stack_name, command, created_at
		FROM jobs
		WHERE status = ?
		ORDER BY priority DESC, created_at ASC
		LIMIT 1
	`, JobStatusPending)

	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Mark as running
	now := time.Now()
	job.StartedAt = &now
	job.Status = JobStatusRunning

	_, err = tx.ExecContext(ctx, `
		UPDATE jobs SET status = ?, started_at = ?
		WHERE id = ?
	`, job.Status, now.Format(time.RFC3339Nano), job.ID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return job, nil
}

// DequeueForGroup gets the next pending job for a specific group
func (s *SQLiteStore) DequeueForGroup(ctx context.Context, groupKey string) (*Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
		SELECT id, input_path, target_type, is_modify, status, priority, group_key, stack_name, command, created_at
		FROM jobs
		WHERE status = ? AND group_key = ?
		ORDER BY priority DESC, created_at ASC
		LIMIT 1
	`, JobStatusPending, groupKey)

	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	now := time.Now()
	job.StartedAt = &now
	job.Status = JobStatusRunning

	_, err = tx.ExecContext(ctx, `
		UPDATE jobs SET status = ?, started_at = ?
		WHERE id = ?
	`, job.Status, now.Format(time.RFC3339Nano), job.ID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return job, nil
}

// Complete marks a job as completed
func (s *SQLiteStore) Complete(ctx context.Context, jobID string, result *JobResult) error {
	now := time.Now()
	var outputPathsJSON *string
	if len(result.OutputPaths) > 0 {
		b, _ := json.Marshal(result.OutputPaths)
		s := string(b)
		outputPathsJSON = &s
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = ?, completed_at = ?, exit_code = ?, duration_ms = ?, stdout = ?, stderr = ?, output_paths = ?
		WHERE id = ?
	`, JobStatusCompleted, now.Format(time.RFC3339Nano), result.ExitCode, result.DurationMs,
		result.Stdout, result.Stderr, outputPathsJSON, jobID)
	return err
}

// Fail marks a job as failed
func (s *SQLiteStore) Fail(ctx context.Context, jobID string, result *JobResult) error {
	now := time.Now()
	errStr := ""
	if result.Error != nil {
		errStr = result.Error.Error()
	}
	var outputPathsJSON *string
	if len(result.OutputPaths) > 0 {
		b, _ := json.Marshal(result.OutputPaths)
		s := string(b)
		outputPathsJSON = &s
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = ?, completed_at = ?, exit_code = ?, duration_ms = ?, stdout = ?, stderr = ?, error = ?, output_paths = ?
		WHERE id = ?
	`, JobStatusFailed, now.Format(time.RFC3339Nano), result.ExitCode, result.DurationMs,
		result.Stdout, result.Stderr, errStr, outputPathsJSON, jobID)
	return err
}

// Get retrieves a job by ID
func (s *SQLiteStore) Get(ctx context.Context, jobID string) (*Job, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, input_path, target_type, is_modify, status, priority, group_key, stack_name, instance_id, claimed_by, command, created_at,
		       started_at, completed_at, exit_code, duration_ms, output_paths, error, stdout, stderr
		FROM jobs
		WHERE id = ?
	`, jobID)

	return scanFullJob(row)
}

// GetByInputPath retrieves a pending or running job by input path
func (s *SQLiteStore) GetByInputPath(ctx context.Context, inputPath string) (*Job, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, input_path, target_type, is_modify, status, priority, group_key, stack_name, instance_id, claimed_by, command, created_at,
		       started_at, completed_at, exit_code, duration_ms, output_paths, error, stdout, stderr
		FROM jobs
		WHERE input_path = ? AND status IN (?, ?)
		LIMIT 1
	`, inputPath, JobStatusPending, JobStatusRunning)

	job, err := scanFullJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return job, err
}

// ListPending returns pending jobs
func (s *SQLiteStore) ListPending(ctx context.Context, limit int) ([]JobSummary, error) {
	return s.listByStatus(ctx, JobStatusPending, limit)
}

// ListFailed returns failed jobs
func (s *SQLiteStore) ListFailed(ctx context.Context, limit int) ([]JobSummary, error) {
	return s.listByStatus(ctx, JobStatusFailed, limit)
}

// ListRunning returns running jobs
func (s *SQLiteStore) ListRunning(ctx context.Context) ([]JobSummary, error) {
	return s.listByStatus(ctx, JobStatusRunning, 0)
}

// ListRecentlyCompleted returns recently completed jobs ordered by completion time descending
func (s *SQLiteStore) ListRecentlyCompleted(ctx context.Context, limit int) ([]JobSummary, error) {
	query := `
		SELECT id, input_path, status, priority, group_key, stack_name, instance_id, created_at, completed_at, duration_ms, output_paths, error
		FROM jobs
		WHERE status = ?
		ORDER BY completed_at DESC
	`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.QueryContext(ctx, query, JobStatusCompleted)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanJobSummaries(rows)
}

func (s *SQLiteStore) listByStatus(ctx context.Context, status JobStatus, limit int) ([]JobSummary, error) {
	query := `
		SELECT id, input_path, status, priority, group_key, stack_name, instance_id, created_at, completed_at, duration_ms, output_paths, error
		FROM jobs
		WHERE status = ?
		ORDER BY priority DESC, created_at ASC
	`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.QueryContext(ctx, query, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanJobSummaries(rows)
}

func scanJobSummaries(rows *sql.Rows) ([]JobSummary, error) {
	var summaries []JobSummary
	for rows.Next() {
		var js JobSummary
		var createdAt string
		var priority, durationMs sql.NullInt64
		var groupKey, stackName, instanceID, completedAt, outputPathsJSON, errStr sql.NullString

		if err := rows.Scan(&js.ID, &js.InputPath, &js.Status, &priority, &groupKey, &stackName, &instanceID, &createdAt, &completedAt, &durationMs, &outputPathsJSON, &errStr); err != nil {
			return nil, err
		}

		js.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		if priority.Valid {
			js.Priority = int(priority.Int64)
		}
		if groupKey.Valid {
			js.GroupKey = groupKey.String
		}
		if stackName.Valid {
			js.StackName = stackName.String
		}
		if instanceID.Valid {
			js.InstanceID = instanceID.String
		}
		if completedAt.Valid {
			t, _ := time.Parse(time.RFC3339Nano, completedAt.String)
			js.CompletedAt = &t
		}
		if durationMs.Valid {
			js.DurationMs = &durationMs.Int64
		}
		if outputPathsJSON.Valid {
			json.Unmarshal([]byte(outputPathsJSON.String), &js.OutputPaths)
		}
		if errStr.Valid {
			js.Error = errStr.String
		}

		summaries = append(summaries, js)
	}

	return summaries, rows.Err()
}

// GetStats returns queue statistics
func (s *SQLiteStore) GetStats(ctx context.Context) (*QueueStats, error) {
	stats := &QueueStats{}

	rows, err := s.db.QueryContext(ctx, `
		SELECT status, COUNT(*) FROM jobs GROUP BY status
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}

		switch JobStatus(status) {
		case JobStatusPending:
			stats.Pending = count
		case JobStatusRunning:
			stats.Running = count
		case JobStatusCompleted:
			stats.Completed = count
		case JobStatusFailed:
			stats.Failed = count
		}
		stats.Total += count
	}

	return stats, rows.Err()
}

// GetDistinctGroups returns distinct group keys for pending jobs
func (s *SQLiteStore) GetDistinctGroups(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT group_key FROM jobs
		WHERE status = ? AND group_key IS NOT NULL AND group_key != ''
		ORDER BY group_key
	`, JobStatusPending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []string
	for rows.Next() {
		var group string
		if err := rows.Scan(&group); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}

	return groups, rows.Err()
}

// Retry re-enqueues a failed job
func (s *SQLiteStore) Retry(ctx context.Context, jobID string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = ?, started_at = NULL, completed_at = NULL,
		    exit_code = NULL, duration_ms = NULL, error = NULL, stdout = NULL, stderr = NULL
		WHERE id = ? AND status = ?
	`, JobStatusPending, jobID, JobStatusFailed)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("job %q not found or not in failed state", jobID)
	}

	return nil
}

// RetryAll re-enqueues all failed jobs
func (s *SQLiteStore) RetryAll(ctx context.Context) (int, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = ?, started_at = NULL, completed_at = NULL,
		    exit_code = NULL, duration_ms = NULL, error = NULL, stdout = NULL, stderr = NULL
		WHERE status = ?
	`, JobStatusPending, JobStatusFailed)
	if err != nil {
		return 0, err
	}

	affected, err := result.RowsAffected()
	return int(affected), err
}

// HasPendingOrRunning checks if a job exists for the input path
func (s *SQLiteStore) HasPendingOrRunning(ctx context.Context, inputPath string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM jobs
		WHERE input_path = ? AND status IN (?, ?)
	`, inputPath, JobStatusPending, JobStatusRunning).Scan(&count)
	return count > 0, err
}

// HasPending checks if a pending job exists for the input path
func (s *SQLiteStore) HasPending(ctx context.Context, inputPath string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM jobs
		WHERE input_path = ? AND status = ?
	`, inputPath, JobStatusPending).Scan(&count)
	return count > 0, err
}

// CleanupStaleRunning resets jobs that were running but never completed
func (s *SQLiteStore) CleanupStaleRunning(ctx context.Context) (int, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = ?, started_at = NULL
		WHERE status = ?
	`, JobStatusPending, JobStatusRunning)
	if err != nil {
		return 0, err
	}

	affected, err := result.RowsAffected()
	return int(affected), err
}

// scanJob scans basic job fields from a row
func scanJob(row *sql.Row) (*Job, error) {
	var job Job
	var createdAt string
	var isModify int
	var priority sql.NullInt64
	var targetType, groupKey, stackName, commandJSON sql.NullString

	err := row.Scan(&job.ID, &job.InputPath, &targetType, &isModify, &job.Status, &priority, &groupKey, &stackName, &commandJSON, &createdAt)
	if err != nil {
		return nil, err
	}

	job.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	job.IsModify = isModify != 0
	if priority.Valid {
		job.Priority = int(priority.Int64)
	}
	if targetType.Valid {
		job.TargetType = targetType.String
	}
	if groupKey.Valid {
		job.GroupKey = groupKey.String
	}
	if stackName.Valid {
		job.StackName = stackName.String
	}
	if commandJSON.Valid {
		if err := json.Unmarshal([]byte(commandJSON.String), &job.Command); err != nil {
			return nil, fmt.Errorf("failed to unmarshal command: %w", err)
		}
	}

	return &job, nil
}

// scanFullJob scans all job fields from a row
func scanFullJob(row *sql.Row) (*Job, error) {
	var job Job
	var createdAt string
	var isModify int
	var priority, claimedBy sql.NullInt64
	var targetType, groupKey, stackName, instanceID, commandJSON, startedAt, completedAt, outputPathsJSON, errStr, stdout, stderr sql.NullString
	var exitCode, durationMs sql.NullInt64

	err := row.Scan(
		&job.ID, &job.InputPath, &targetType, &isModify, &job.Status, &priority, &groupKey, &stackName, &instanceID, &claimedBy, &commandJSON, &createdAt,
		&startedAt, &completedAt, &exitCode, &durationMs, &outputPathsJSON, &errStr, &stdout, &stderr,
	)
	if err != nil {
		return nil, err
	}

	job.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	job.IsModify = isModify != 0
	if priority.Valid {
		job.Priority = int(priority.Int64)
	}

	if targetType.Valid {
		job.TargetType = targetType.String
	}
	if groupKey.Valid {
		job.GroupKey = groupKey.String
	}
	if stackName.Valid {
		job.StackName = stackName.String
	}
	if instanceID.Valid {
		job.InstanceID = instanceID.String
	}
	if claimedBy.Valid {
		cb := int(claimedBy.Int64)
		job.ClaimedBy = &cb
	}
	if commandJSON.Valid {
		if err := json.Unmarshal([]byte(commandJSON.String), &job.Command); err != nil {
			return nil, fmt.Errorf("failed to unmarshal command: %w", err)
		}
	}
	if startedAt.Valid {
		t, _ := time.Parse(time.RFC3339Nano, startedAt.String)
		job.StartedAt = &t
	}
	if completedAt.Valid {
		t, _ := time.Parse(time.RFC3339Nano, completedAt.String)
		job.CompletedAt = &t
	}
	if exitCode.Valid {
		ec := int(exitCode.Int64)
		job.ExitCode = &ec
	}
	if durationMs.Valid {
		job.DurationMs = &durationMs.Int64
	}
	if outputPathsJSON.Valid {
		json.Unmarshal([]byte(outputPathsJSON.String), &job.OutputPaths)
	}
	if errStr.Valid {
		job.Error = errStr.String
	}
	if stdout.Valid {
		job.Stdout = stdout.String
	}
	if stderr.Valid {
		job.Stderr = stderr.String
	}

	return &job, nil
}

// Stack mode methods

// GetStackState returns the current stack state
func (s *SQLiteStore) GetStackState(ctx context.Context) (*StackState, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT current_stack, last_switch_at, last_switch_duration_ms
		FROM stack_state
		WHERE id = 1
	`)

	var state StackState
	var currentStack, lastSwitchAt sql.NullString
	var lastSwitchDurationMs sql.NullInt64

	err := row.Scan(&currentStack, &lastSwitchAt, &lastSwitchDurationMs)
	if err == sql.ErrNoRows {
		return &StackState{}, nil
	}
	if err != nil {
		return nil, err
	}

	if currentStack.Valid {
		state.CurrentStack = currentStack.String
	}
	if lastSwitchAt.Valid {
		t, _ := time.Parse(time.RFC3339Nano, lastSwitchAt.String)
		state.LastSwitchAt = &t
	}
	if lastSwitchDurationMs.Valid {
		state.LastSwitchDurationMs = lastSwitchDurationMs.Int64
	}

	return &state, nil
}

// SetCurrentStack updates the current stack and records switch duration
func (s *SQLiteStore) SetCurrentStack(ctx context.Context, stackName string, switchDurationMs int64) error {
	now := time.Now()

	// Upsert the stack state
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO stack_state (id, current_stack, last_switch_at, last_switch_duration_ms)
		VALUES (1, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			current_stack = excluded.current_stack,
			last_switch_at = excluded.last_switch_at,
			last_switch_duration_ms = excluded.last_switch_duration_ms
	`, stackName, now.Format(time.RFC3339Nano), switchDurationMs)
	if err != nil {
		return err
	}

	// Update stack statistics
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO stack_stats (stack_name, switch_count, total_switch_duration_ms)
		VALUES (?, 1, ?)
		ON CONFLICT(stack_name) DO UPDATE SET
			switch_count = stack_stats.switch_count + 1,
			total_switch_duration_ms = stack_stats.total_switch_duration_ms + excluded.total_switch_duration_ms
	`, stackName, switchDurationMs)

	return err
}

// UpdateStackJobStats records a job completion for stack statistics
func (s *SQLiteStore) UpdateStackJobStats(ctx context.Context, stackName string, jobDurationMs int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO stack_stats (stack_name, job_count, total_job_duration_ms)
		VALUES (?, 1, ?)
		ON CONFLICT(stack_name) DO UPDATE SET
			job_count = stack_stats.job_count + 1,
			total_job_duration_ms = stack_stats.total_job_duration_ms + excluded.total_job_duration_ms
	`, stackName, jobDurationMs)
	return err
}

// GetPendingCountByStack returns pending job counts grouped by stack
func (s *SQLiteStore) GetPendingCountByStack(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(stack_name, '') as stack, COUNT(*) as count
		FROM jobs
		WHERE status = ?
		GROUP BY stack_name
	`, JobStatusPending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var stack string
		var count int
		if err := rows.Scan(&stack, &count); err != nil {
			return nil, err
		}
		result[stack] = count
	}

	return result, rows.Err()
}

// DequeueForStack gets the next pending job for a specific stack
func (s *SQLiteStore) DequeueForStack(ctx context.Context, stackName string) (*Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Handle empty stack name (NULL in DB)
	var row *sql.Row
	if stackName == "" {
		row = tx.QueryRowContext(ctx, `
			SELECT id, input_path, target_type, is_modify, status, priority, group_key, stack_name, command, created_at
			FROM jobs
			WHERE status = ? AND (stack_name IS NULL OR stack_name = '')
			ORDER BY priority DESC, created_at ASC
			LIMIT 1
		`, JobStatusPending)
	} else {
		row = tx.QueryRowContext(ctx, `
			SELECT id, input_path, target_type, is_modify, status, priority, group_key, stack_name, command, created_at
			FROM jobs
			WHERE status = ? AND stack_name = ?
			ORDER BY priority DESC, created_at ASC
			LIMIT 1
		`, JobStatusPending, stackName)
	}

	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	now := time.Now()
	job.StartedAt = &now
	job.Status = JobStatusRunning

	_, err = tx.ExecContext(ctx, `
		UPDATE jobs SET status = ?, started_at = ?
		WHERE id = ?
	`, job.Status, now.Format(time.RFC3339Nano), job.ID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return job, nil
}

// GetStackStats returns statistics for all stacks
func (s *SQLiteStore) GetStackStats(ctx context.Context) ([]StackStats, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT stack_name, job_count, total_job_duration_ms, switch_count, total_switch_duration_ms
		FROM stack_stats
		ORDER BY stack_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []StackStats
	for rows.Next() {
		var ss StackStats
		if err := rows.Scan(&ss.StackName, &ss.JobCount, &ss.TotalJobDurationMs, &ss.SwitchCount, &ss.TotalSwitchDurationMs); err != nil {
			return nil, err
		}
		// Compute averages
		if ss.JobCount > 0 {
			ss.AvgJobDurationMs = ss.TotalJobDurationMs / int64(ss.JobCount)
		}
		if ss.SwitchCount > 0 {
			ss.AvgSwitchDurationMs = ss.TotalSwitchDurationMs / int64(ss.SwitchCount)
		}
		stats = append(stats, ss)
	}

	return stats, rows.Err()
}

// Priority management methods

// SetPriority sets the priority of a pending job
func (s *SQLiteStore) SetPriority(ctx context.Context, jobID string, priority int) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE jobs SET priority = ?
		WHERE id = ? AND status = ?
	`, priority, jobID, JobStatusPending)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("job %q not found or not in pending state", jobID)
	}

	return nil
}

// GetMaxPriority returns the maximum priority among pending jobs
func (s *SQLiteStore) GetMaxPriority(ctx context.Context) (int, error) {
	var maxPriority sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT MAX(priority) FROM jobs WHERE status = ?
	`, JobStatusPending).Scan(&maxPriority)
	if err != nil {
		return 0, err
	}
	if !maxPriority.Valid {
		return 0, nil
	}
	return int(maxPriority.Int64), nil
}

// GetMinPriority returns the minimum priority among pending jobs
func (s *SQLiteStore) GetMinPriority(ctx context.Context) (int, error) {
	var minPriority sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT MIN(priority) FROM jobs WHERE status = ?
	`, JobStatusPending).Scan(&minPriority)
	if err != nil {
		return 0, err
	}
	if !minPriority.Valid {
		return 0, nil
	}
	return int(minPriority.Int64), nil
}

// GetNextPendingJob returns the next job that would be dequeued (without marking it as running)
func (s *SQLiteStore) GetNextPendingJob(ctx context.Context) (*Job, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, input_path, target_type, is_modify, status, priority, group_key, stack_name, instance_id, claimed_by, command, created_at,
		       started_at, completed_at, exit_code, duration_ms, output_paths, error, stdout, stderr
		FROM jobs
		WHERE status = ?
		ORDER BY priority DESC, created_at ASC
		LIMIT 1
	`, JobStatusPending)

	job, err := scanFullJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return job, err
}

// Process tracking methods

// RegisterProcess registers a running filehook process
func (s *SQLiteStore) RegisterProcess(ctx context.Context, info *ProcessInfo) error {
	role := info.Role
	if role == "" {
		role = ProcessRoleLegacy
	}
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO process_info (pid, command, started_at, role, instance_id, heartbeat_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, info.PID, info.Command, info.StartedAt.Format(time.RFC3339Nano), string(role), info.InstanceID, now.Format(time.RFC3339Nano))
	return err
}

// UnregisterProcess removes a process registration
func (s *SQLiteStore) UnregisterProcess(ctx context.Context, pid int) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM process_info WHERE pid = ?
	`, pid)
	return err
}

// GetActiveProcess returns the currently registered process, or nil if none is alive
func (s *SQLiteStore) GetActiveProcess(ctx context.Context) (*ProcessInfo, error) {
	processes, err := s.getAllProcesses(ctx)
	if err != nil {
		return nil, err
	}

	for _, info := range processes {
		if isProcessAliveByHeartbeat(info) {
			return &info, nil
		}
		s.db.ExecContext(ctx, "DELETE FROM process_info WHERE pid = ?", info.PID)
	}

	return nil, nil
}

// GetActiveProcesses returns all live registered processes.
// Liveness is determined by heartbeat recency (within 90s) rather than
// OS-level process checks, so this works across containers sharing a database.
func (s *SQLiteStore) GetActiveProcesses(ctx context.Context) ([]ProcessInfo, error) {
	processes, err := s.getAllProcesses(ctx)
	if err != nil {
		return nil, err
	}

	var alive []ProcessInfo
	for _, info := range processes {
		if isProcessAliveByHeartbeat(info) {
			alive = append(alive, info)
		} else {
			s.db.ExecContext(ctx, "DELETE FROM process_info WHERE pid = ?", info.PID)
		}
	}

	return alive, nil
}

// GetSchedulerProcess returns the live scheduler process, or nil
func (s *SQLiteStore) GetSchedulerProcess(ctx context.Context) (*ProcessInfo, error) {
	processes, err := s.getAllProcesses(ctx)
	if err != nil {
		return nil, err
	}

	for _, info := range processes {
		if info.Role == ProcessRoleScheduler {
			if isProcessAliveByHeartbeat(info) {
				return &info, nil
			}
			s.db.ExecContext(ctx, "DELETE FROM process_info WHERE pid = ?", info.PID)
		}
	}

	return nil, nil
}

// UpdateHeartbeat updates the heartbeat timestamp for a process
func (s *SQLiteStore) UpdateHeartbeat(ctx context.Context, pid int) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
		UPDATE process_info SET heartbeat_at = ? WHERE pid = ?
	`, now.Format(time.RFC3339Nano), pid)
	return err
}

func (s *SQLiteStore) getAllProcesses(ctx context.Context) ([]ProcessInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT pid, command, started_at, role, instance_id, heartbeat_at FROM process_info
	`)
	if err != nil {
		return nil, err
	}

	var processes []ProcessInfo
	for rows.Next() {
		var info ProcessInfo
		var startedAt string
		var role, instanceID, heartbeatAt sql.NullString
		if err := rows.Scan(&info.PID, &info.Command, &startedAt, &role, &instanceID, &heartbeatAt); err != nil {
			rows.Close()
			return nil, err
		}
		info.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAt)
		if role.Valid {
			info.Role = ProcessRole(role.String)
		} else {
			info.Role = ProcessRoleLegacy
		}
		if instanceID.Valid {
			info.InstanceID = instanceID.String
		}
		if heartbeatAt.Valid {
			info.HeartbeatAt, _ = time.Parse(time.RFC3339Nano, heartbeatAt.String)
		}
		processes = append(processes, info)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	return processes, nil
}

// DequeueWithClaim dequeues the next pending job and sets claimed_by
func (s *SQLiteStore) DequeueWithClaim(ctx context.Context, claimerPID int) (*Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
		SELECT id, input_path, target_type, is_modify, status, priority, group_key, stack_name, command, created_at
		FROM jobs
		WHERE status = ?
		ORDER BY priority DESC, created_at ASC
		LIMIT 1
	`, JobStatusPending)

	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	now := time.Now()
	job.StartedAt = &now
	job.Status = JobStatusRunning
	job.ClaimedBy = &claimerPID

	_, err = tx.ExecContext(ctx, `
		UPDATE jobs SET status = ?, started_at = ?, claimed_by = ?
		WHERE id = ?
	`, job.Status, now.Format(time.RFC3339Nano), claimerPID, job.ID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return job, nil
}

// DequeueForStackWithClaim dequeues the next pending job for a stack and sets claimed_by
func (s *SQLiteStore) DequeueForStackWithClaim(ctx context.Context, stackName string, claimerPID int) (*Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var row *sql.Row
	if stackName == "" {
		row = tx.QueryRowContext(ctx, `
			SELECT id, input_path, target_type, is_modify, status, priority, group_key, stack_name, command, created_at
			FROM jobs
			WHERE status = ? AND (stack_name IS NULL OR stack_name = '')
			ORDER BY priority DESC, created_at ASC
			LIMIT 1
		`, JobStatusPending)
	} else {
		row = tx.QueryRowContext(ctx, `
			SELECT id, input_path, target_type, is_modify, status, priority, group_key, stack_name, command, created_at
			FROM jobs
			WHERE status = ? AND stack_name = ?
			ORDER BY priority DESC, created_at ASC
			LIMIT 1
		`, JobStatusPending, stackName)
	}

	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	now := time.Now()
	job.StartedAt = &now
	job.Status = JobStatusRunning
	job.ClaimedBy = &claimerPID

	_, err = tx.ExecContext(ctx, `
		UPDATE jobs SET status = ?, started_at = ?, claimed_by = ?
		WHERE id = ?
	`, job.Status, now.Format(time.RFC3339Nano), claimerPID, job.ID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return job, nil
}

// CleanupStaleRunningForPID resets running jobs claimed by a specific dead PID
func (s *SQLiteStore) CleanupStaleRunningForPID(ctx context.Context, pid int) (int, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = ?, started_at = NULL, claimed_by = NULL
		WHERE status = ? AND claimed_by = ?
	`, JobStatusPending, JobStatusRunning, pid)
	if err != nil {
		return 0, err
	}

	affected, err := result.RowsAffected()
	return int(affected), err
}

// ListPendingByInstance returns pending jobs filtered by instance ID
func (s *SQLiteStore) ListPendingByInstance(ctx context.Context, instanceID string, limit int) ([]JobSummary, error) {
	query := `
		SELECT id, input_path, status, priority, group_key, stack_name, instance_id, created_at, completed_at, duration_ms, output_paths, error
		FROM jobs
		WHERE status = ? AND instance_id = ?
		ORDER BY priority DESC, created_at ASC
	`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.QueryContext(ctx, query, JobStatusPending, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanJobSummaries(rows)
}

// SetPriorityByInstance sets priority only if the job belongs to the given instance
func (s *SQLiteStore) SetPriorityByInstance(ctx context.Context, jobID string, instanceID string, priority int) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE jobs SET priority = ?
		WHERE id = ? AND status = ? AND instance_id = ?
	`, priority, jobID, JobStatusPending, instanceID)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("job %q not found, not pending, or not from instance %q", jobID, instanceID)
	}

	return nil
}

// GetStatsByInstance returns queue stats filtered by instance ID
func (s *SQLiteStore) GetStatsByInstance(ctx context.Context, instanceID string) (*QueueStats, error) {
	stats := &QueueStats{}

	rows, err := s.db.QueryContext(ctx, `
		SELECT status, COUNT(*) FROM jobs WHERE instance_id = ? GROUP BY status
	`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}

		switch JobStatus(status) {
		case JobStatusPending:
			stats.Pending = count
		case JobStatusRunning:
			stats.Running = count
		case JobStatusCompleted:
			stats.Completed = count
		case JobStatusFailed:
			stats.Failed = count
		}
		stats.Total += count
	}

	return stats, rows.Err()
}

// heartbeatTimeout is how long since the last heartbeat before a process is
// considered dead. The heartbeat interval is 30s, so 90s allows for 2 missed beats.
const heartbeatTimeout = 90 * time.Second

// isProcessAliveByHeartbeat checks whether a process has sent a recent heartbeat.
// This works across containers that share the same SQLite database.
func isProcessAliveByHeartbeat(info ProcessInfo) bool {
	if info.HeartbeatAt.IsZero() {
		// No heartbeat recorded — fall back to started_at for processes
		// that just registered but haven't ticked yet
		return time.Since(info.StartedAt) < heartbeatTimeout
	}
	return time.Since(info.HeartbeatAt) < heartbeatTimeout
}
