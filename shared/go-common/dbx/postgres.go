// Package dbx wraps pgx connection setup with sane defaults.
package dbx

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config holds Postgres connection parameters.
type Config struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	HealthCheck     time.Duration
}

// DefaultConfig returns a config tuned for a small service.
func DefaultConfig(url string) Config {
	return Config{
		URL:             url,
		MaxConns:        10,
		MinConns:        1,
		MaxConnLifetime: 30 * time.Minute,
		HealthCheck:     30 * time.Second,
	}
}

// Connect builds a pgxpool from the config and pings to verify connectivity.
func Connect(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	pcfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse pg url: %w", err)
	}
	pcfg.MaxConns = cfg.MaxConns
	pcfg.MinConns = cfg.MinConns
	pcfg.MaxConnLifetime = cfg.MaxConnLifetime
	pcfg.HealthCheckPeriod = cfg.HealthCheck

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}
