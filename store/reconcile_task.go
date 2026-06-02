package store

import (
	"context"
	"encoding/json"
	"time"
)

type ReconcileTask struct {
	Version    string          `json:"version"`
	Status     string          `json:"status"`
	PodID      string          `json:"pod_id"`
	StartedAt  *time.Time      `json:"started_at,omitempty"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// TryAcquireReconcileTask attempts to claim a reconcile task for a given version.
// Uses optimistic locking: only succeeds if the task doesn't exist, is not currently running,
// or has been running for longer than the stale timeout (10 minutes).
// Returns true if the lock was acquired.
func (s *PGStore) TryAcquireReconcileTask(ctx context.Context, version, podID string) (bool, error) {
	q := `INSERT INTO reconcile_task (version, status, pod_id, started_at, updated_at)
		VALUES ($1, 'running', $2, NOW(), NOW())
		ON CONFLICT (version) DO UPDATE
			SET status = 'running', pod_id = $2, started_at = NOW(), updated_at = NOW()
			WHERE reconcile_task.status != 'running'
			   OR reconcile_task.updated_at < NOW() - INTERVAL '10 minutes'`
	tag, err := s.pool.Exec(ctx, q, version, podID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// HeartbeatReconcileTask updates the updated_at timestamp to prevent stale timeout.
func (s *PGStore) HeartbeatReconcileTask(ctx context.Context, version, podID string) error {
	q := `UPDATE reconcile_task SET updated_at = NOW()
		WHERE version = $1 AND pod_id = $2 AND status = 'running'`
	_, err := s.pool.Exec(ctx, q, version, podID)
	return err
}

// CompleteReconcileTask marks a task as done with its result.
func (s *PGStore) CompleteReconcileTask(ctx context.Context, version, podID string, result json.RawMessage) error {
	q := `UPDATE reconcile_task
		SET status = 'done', finished_at = NOW(), result = $3, updated_at = NOW()
		WHERE version = $1 AND pod_id = $2 AND status = 'running'`
	_, err := s.pool.Exec(ctx, q, version, podID, result)
	return err
}

// FailReconcileTask marks a task as failed with an error result.
func (s *PGStore) FailReconcileTask(ctx context.Context, version, podID string, result json.RawMessage) error {
	q := `UPDATE reconcile_task
		SET status = 'failed', finished_at = NOW(), result = $3, updated_at = NOW()
		WHERE version = $1 AND pod_id = $2 AND status = 'running'`
	_, err := s.pool.Exec(ctx, q, version, podID, result)
	return err
}

// GetReconcileTask returns the task for a given version, or nil if not found.
func (s *PGStore) GetReconcileTask(ctx context.Context, version string) (*ReconcileTask, error) {
	q := `SELECT version, status, pod_id, started_at, finished_at, result, updated_at
		FROM reconcile_task WHERE version = $1`
	row := s.pool.QueryRow(ctx, q, version)
	var t ReconcileTask
	err := row.Scan(&t.Version, &t.Status, &t.PodID, &t.StartedAt, &t.FinishedAt, &t.Result, &t.UpdatedAt)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

// GetLatestReconcileTask returns the most recently updated reconcile task.
func (s *PGStore) GetLatestReconcileTask(ctx context.Context) (*ReconcileTask, error) {
	q := `SELECT version, status, pod_id, started_at, finished_at, result, updated_at
		FROM reconcile_task ORDER BY updated_at DESC LIMIT 1`
	row := s.pool.QueryRow(ctx, q)
	var t ReconcileTask
	err := row.Scan(&t.Version, &t.Status, &t.PodID, &t.StartedAt, &t.FinishedAt, &t.Result, &t.UpdatedAt)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

// ListReconcileVersions returns all versions that have been successfully reconciled.
func (s *PGStore) ListReconcileVersions(ctx context.Context) ([]string, error) {
	q := `SELECT version FROM reconcile_task WHERE status = 'done' ORDER BY finished_at DESC`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var versions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}
