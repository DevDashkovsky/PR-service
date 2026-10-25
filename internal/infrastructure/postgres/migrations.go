package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	gooselock "github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var migrations embed.FS

func (db *Postgres) RunMigrations(ctx context.Context) error {
	config, err := pgxpool.ParseConfig(db.dsn)
	if err != nil {
		return fmt.Errorf("parse database configuration for migrations: %w", err)
	}
	// Pool settings must not be sent to PostgreSQL as session parameters.
	sqlDB := stdlib.OpenDB(*config.ConnConfig)
	defer func() { _ = sqlDB.Close() }()

	migrationFS, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("open embedded migrations: %w", err)
	}
	locker, err := gooselock.NewPostgresSessionLocker(gooselock.WithLockTimeout(1, 30))
	if err != nil {
		return fmt.Errorf("create migration locker: %w", err)
	}
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		sqlDB,
		migrationFS,
		goose.WithSessionLocker(locker),
	)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}

	migrationCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if _, err := provider.Up(migrationCtx); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
