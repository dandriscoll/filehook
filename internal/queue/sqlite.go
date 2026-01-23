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
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(1) // SQLite doesn't support multiple writers
	db.SetMaxIdleConns(1)

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
	`

	_, err := s.db.ExecContext(ctx, schema)
	if err != nil {
		return err
	}

	// Migration: add target_type and is_modify columns if they don't exist
	// Also drop output_paths column if it exists (data migration not needed as outputs are now calculated at runtime)
	migrations := []string{
		"ALTER TABLE jobs ADD COLUMN target_type TEXT",
		"ALTER TABLE jobs ADD COLUMN is_modify INTEGER DEFAULT 0",
	}

	for _, migration := range migrations {
		// Ignore errors - column may already exist
		s.db.ExecContext(ctx, migration)
	}

	return nil
}

// Close closes the database connection
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// Enqueue adds a new job to the queue
func (s *SQLiteStore) Enqueue(ctx context.Context, job *Job) error {
	// Check for existing pending/running job
	exists, err := s.HasPendingOrRunning(ctx, job.InputPath)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("job for %q is already pending or running", job.InputPath)
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

	isModify := 0
	if job.IsModify {
		isModify = 1
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO jobs (id, input_path, target_type, is_modify, status, group_key, command, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, job.ID, job.InputPath, targetType, isModify, job.Status, job.GroupKey, commandJSON, job.CreatedAt.Format(time.RFC3339Nano))

	return err
}

// Dequeue gets the next pending job and marks it as running
func (s *SQLiteStore) Dequeue(ctx context.Context) (*Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Get oldest pending job
	row := tx.QueryRowContext(ctx, `
		SELECT id, input_path, target_type, is_modify, status, group_key, command, created_at
		FROM jobs
		WHERE status = ?
		ORDER BY created_at ASC
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
		SELECT id, input_path, target_type, is_modify, status, group_key, command, created_at
		FROM jobs
		WHERE status = ? AND group_key = ?
		ORDER BY created_at ASC
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
	_, err := s.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = ?, completed_at = ?, exit_code = ?, duration_ms = ?, stdout = ?, stderr = ?
		WHERE id = ?
	`, JobStatusCompleted, now.Format(time.RFC3339Nano), result.ExitCode, result.DurationMs,
		result.Stdout, result.Stderr, jobID)
	return err
}

// Fail marks a job as failed
func (s *SQLiteStore) Fail(ctx context.Context, jobID string, result *JobResult) error {
	now := time.Now()
	errStr := ""
	if result.Error != nil {
		errStr = result.Error.Error()
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = ?, completed_at = ?, exit_code = ?, duration_ms = ?, stdout = ?, stderr = ?, error = ?
		WHERE id = ?
	`, JobStatusFailed, now.Format(time.RFC3339Nano), result.ExitCode, result.DurationMs,
		result.Stdout, result.Stderr, errStr, jobID)
	return err
}

// Get retrieves a job by ID
func (s *SQLiteStore) Get(ctx context.Context, jobID string) (*Job, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, input_path, target_type, is_modify, status, group_key, command, created_at,
		       started_at, completed_at, exit_code, duration_ms, error, stdout, stderr
		FROM jobs
		WHERE id = ?
	`, jobID)

	return scanFullJob(row)
}

// GetByInputPath retrieves a pending or running job by input path
func (s *SQLiteStore) GetByInputPath(ctx context.Context, inputPath string) (*Job, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, input_path, target_type, is_modify, status, group_key, command, created_at,
		       started_at, completed_at, exit_code, duration_ms, error, stdout, stderr
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

func (s *SQLiteStore) listByStatus(ctx context.Context, status JobStatus, limit int) ([]JobSummary, error) {
	query := `
		SELECT id, input_path, status, group_key, created_at, error
		FROM jobs
		WHERE status = ?
		ORDER BY created_at ASC
	`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.QueryContext(ctx, query, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []JobSummary
	for rows.Next() {
		var js JobSummary
		var createdAt string
		var groupKey, errStr sql.NullString

		if err := rows.Scan(&js.ID, &js.InputPath, &js.Status, &groupKey, &createdAt, &errStr); err != nil {
			return nil, err
		}

		js.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		if groupKey.Valid {
			js.GroupKey = groupKey.String
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
	var targetType, groupKey, commandJSON sql.NullString

	err := row.Scan(&job.ID, &job.InputPath, &targetType, &isModify, &job.Status, &groupKey, &commandJSON, &createdAt)
	if err != nil {
		return nil, err
	}

	job.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	job.IsModify = isModify != 0
	if targetType.Valid {
		job.TargetType = targetType.String
	}
	if groupKey.Valid {
		job.GroupKey = groupKey.String
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
	var targetType, groupKey, commandJSON, startedAt, completedAt, errStr, stdout, stderr sql.NullString
	var exitCode, durationMs sql.NullInt64

	err := row.Scan(
		&job.ID, &job.InputPath, &targetType, &isModify, &job.Status, &groupKey, &commandJSON, &createdAt,
		&startedAt, &completedAt, &exitCode, &durationMs, &errStr, &stdout, &stderr,
	)
	if err != nil {
		return nil, err
	}

	job.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	job.IsModify = isModify != 0

	if targetType.Valid {
		job.TargetType = targetType.String
	}
	if groupKey.Valid {
		job.GroupKey = groupKey.String
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
