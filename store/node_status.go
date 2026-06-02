package store

import (
	"context"
	"fmt"
	"time"
)

type NodeStatus struct {
	AgentID        string    `json:"agent_id"`
	System         string    `json:"system"`
	LastSeenAt     time.Time `json:"last_seen_at"`
	GoalVersion    string    `json:"goal_version"`
	CurrentVersion string    `json:"current_version"`
	Status         string    `json:"status"`
	LastError      string    `json:"last_error"`
	CreatedAt      time.Time `json:"created_at"`
}

func (s *PGStore) UpsertNodeStatus(ctx context.Context, ns NodeStatus) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO node_status (agent_id, system, last_seen_at, goal_version, current_version, status, last_error, deleted_at)
		VALUES ($1, $2, NOW(), $3, $4, $5, $6, 0)
		ON CONFLICT (agent_id, deleted_at) DO UPDATE SET
			system = EXCLUDED.system,
			last_seen_at = NOW(),
			goal_version = EXCLUDED.goal_version,
			current_version = EXCLUDED.current_version,
			status = EXCLUDED.status,
			last_error = EXCLUDED.last_error`,
		ns.AgentID, ns.System, ns.GoalVersion, ns.CurrentVersion, ns.Status, ns.LastError,
	)
	if err != nil {
		return fmt.Errorf("upsert node_status: %w", err)
	}
	return nil
}

func (s *PGStore) ListNodeStatus(ctx context.Context, system string) ([]NodeStatus, error) {
	query := `SELECT agent_id, system, last_seen_at, goal_version, current_version, status, last_error, created_at
		FROM node_status WHERE deleted_at = 0`
	args := []any{}
	if system != "" {
		query += ` AND system = $1`
		args = append(args, system)
	}
	query += ` ORDER BY last_seen_at DESC`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list node_status: %w", err)
	}
	defer rows.Close()

	var results []NodeStatus
	for rows.Next() {
		var ns NodeStatus
		if err := rows.Scan(&ns.AgentID, &ns.System, &ns.LastSeenAt, &ns.GoalVersion, &ns.CurrentVersion, &ns.Status, &ns.LastError, &ns.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan node_status: %w", err)
		}
		results = append(results, ns)
	}
	return results, rows.Err()
}

func (s *PGStore) CleanupExpired(ctx context.Context, ttl time.Duration) (int64, error) {
	now := time.Now().Unix()
	tag, err := s.pool.Exec(ctx, `
		UPDATE node_status SET deleted_at = $1
		WHERE deleted_at = 0 AND last_seen_at < NOW() - $2::interval`,
		now, ttl.String(),
	)
	if err != nil {
		return 0, fmt.Errorf("cleanup node_status: %w", err)
	}
	return tag.RowsAffected(), nil
}
