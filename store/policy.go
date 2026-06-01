package store

import (
	"context"
	"fmt"
	"time"
)

type AgentPolicy struct {
	System     string    `json:"system"`
	AutoUpdate bool      `json:"auto_update"`
	UpdatedAt  time.Time `json:"updated_at"`
	UpdatedBy  string    `json:"updated_by"`
}

type SubmapPolicy struct {
	AutoUpdate bool      `json:"auto_update"`
	UpdatedAt  time.Time `json:"updated_at"`
	UpdatedBy  string    `json:"updated_by"`
}

func (s *PGStore) GetAgentPolicy(ctx context.Context, system string) (*AgentPolicy, error) {
	var p AgentPolicy
	err := s.pool.QueryRow(ctx,
		`SELECT system, auto_update, updated_at, updated_by FROM agent_policy WHERE system = $1`, system,
	).Scan(&p.System, &p.AutoUpdate, &p.UpdatedAt, &p.UpdatedBy)
	if err != nil {
		return nil, fmt.Errorf("get agent_policy: %w", err)
	}
	return &p, nil
}

func (s *PGStore) SetAgentPolicy(ctx context.Context, p AgentPolicy) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_policy (system, auto_update, updated_at, updated_by)
		VALUES ($1, $2, NOW(), $3)
		ON CONFLICT (system) DO UPDATE SET
			auto_update = EXCLUDED.auto_update,
			updated_at = NOW(),
			updated_by = EXCLUDED.updated_by`,
		p.System, p.AutoUpdate, p.UpdatedBy,
	)
	if err != nil {
		return fmt.Errorf("set agent_policy: %w", err)
	}
	return nil
}

func (s *PGStore) GetSubmapPolicy(ctx context.Context) (*SubmapPolicy, error) {
	var p SubmapPolicy
	err := s.pool.QueryRow(ctx,
		`SELECT auto_update, updated_at, updated_by FROM submap_policy WHERE id = 'default'`,
	).Scan(&p.AutoUpdate, &p.UpdatedAt, &p.UpdatedBy)
	if err != nil {
		return nil, fmt.Errorf("get submap_policy: %w", err)
	}
	return &p, nil
}

func (s *PGStore) SetSubmapPolicy(ctx context.Context, p SubmapPolicy) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO submap_policy (id, auto_update, updated_at, updated_by)
		VALUES ('default', $1, NOW(), $2)
		ON CONFLICT (id) DO UPDATE SET
			auto_update = EXCLUDED.auto_update,
			updated_at = NOW(),
			updated_by = EXCLUDED.updated_by`,
		p.AutoUpdate, p.UpdatedBy,
	)
	if err != nil {
		return fmt.Errorf("set submap_policy: %w", err)
	}
	return nil
}
