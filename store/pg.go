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
			agent_id        TEXT NOT NULL PRIMARY KEY,
			system          TEXT NOT NULL,
			last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			goal_version    TEXT NOT NULL DEFAULT '',
			current_version TEXT NOT NULL DEFAULT '',
			status          TEXT NOT NULL DEFAULT 'unknown',
			last_error      TEXT NOT NULL DEFAULT '',
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_node_status_system ON node_status(system)`,
		`CREATE INDEX IF NOT EXISTS idx_node_status_last_seen ON node_status(last_seen_at)`,
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
