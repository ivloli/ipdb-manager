package store

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PGStore struct {
	pool *pgxpool.Pool
}

func NewPGStore(ctx context.Context, dsn string) (*PGStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	log.Printf("[store] connected to postgres")
	return &PGStore{pool: pool}, nil
}

func (s *PGStore) Migrate(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS node_status (
			id              BIGSERIAL PRIMARY KEY,
			agent_id        TEXT NOT NULL,
			system          TEXT NOT NULL,
			last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			goal_version    TEXT NOT NULL DEFAULT '',
			current_version TEXT NOT NULL DEFAULT '',
			status          TEXT NOT NULL DEFAULT 'unknown',
			last_error      TEXT NOT NULL DEFAULT '',
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			deleted_at      BIGINT NOT NULL DEFAULT 0
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_node_status_agent_deleted ON node_status(agent_id, deleted_at)`,
		`CREATE INDEX IF NOT EXISTS idx_node_status_system ON node_status(system) WHERE deleted_at = 0`,
		`CREATE INDEX IF NOT EXISTS idx_node_status_last_seen ON node_status(last_seen_at) WHERE deleted_at = 0`,
		`CREATE TABLE IF NOT EXISTS agent_policy (
			system      TEXT PRIMARY KEY,
			auto_update BOOLEAN NOT NULL DEFAULT TRUE,
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_by  TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS submap_policy (
			id          TEXT PRIMARY KEY DEFAULT 'default',
			auto_update BOOLEAN NOT NULL DEFAULT FALSE,
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_by  TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS reconcile_task (
			version     TEXT PRIMARY KEY,
			status      TEXT NOT NULL DEFAULT 'pending',
			pod_id      TEXT NOT NULL DEFAULT '',
			started_at  TIMESTAMPTZ,
			finished_at TIMESTAMPTZ,
			result      JSONB,
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_reconcile_task_status ON reconcile_task(status)`,
		`CREATE INDEX IF NOT EXISTS idx_reconcile_task_updated ON reconcile_task(updated_at)`,
	}
	for _, q := range queries {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	log.Printf("[store] migration completed")
	return nil
}

func (s *PGStore) Close() {
	s.pool.Close()
}
