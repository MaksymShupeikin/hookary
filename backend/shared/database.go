package shared

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// OpenDatabase builds the connection pool and proves it works before returning.
//
// The pool ceiling is explicit and comes from config: five services share one
// Postgres, and pgx's default of 4×CPU per service would exhaust
// max_connections long before any of them is actually busy.
func OpenDatabase(ctx context.Context, cfg Config, logger zerolog.Logger) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}

	poolCfg.MaxConns = cfg.DatabaseMaxConns
	poolCfg.MinConns = 0
	poolCfg.MaxConnLifetime = time.Hour
	poolCfg.MaxConnIdleTime = 5 * time.Minute
	poolCfg.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	logger.Info().
		Int32("max_conns", poolCfg.MaxConns).
		Str("host", poolCfg.ConnConfig.Host).
		Str("database", poolCfg.ConnConfig.Database).
		Msg("database connected")

	return pool, nil
}

// DatabaseProbe reports the pool's health for /health/ready.
func DatabaseProbe(pool *pgxpool.Pool) ReadyFunc {
	return func(ctx context.Context) error {
		if err := pool.Ping(ctx); err != nil {
			return fmt.Errorf("database: %w", err)
		}
		return nil
	}
}
