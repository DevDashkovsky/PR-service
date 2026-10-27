package postgres

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRunMigrationsAdoptsExistingSchema(t *testing.T) {
	t.Run("plain connection", func(t *testing.T) {
		testRunMigrationsAdoptsExistingSchema(t, false)
	})
	t.Run("connection with pool settings", func(t *testing.T) {
		testRunMigrationsAdoptsExistingSchema(t, true)
	})
}

//nolint:gocyclo // The integration setup intentionally verifies every migration boundary.
func testRunMigrationsAdoptsExistingSchema(t *testing.T, poolSettings bool) {
	t.Helper()
	dsn := os.Getenv("PR_SERVICE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set PR_SERVICE_TEST_DATABASE_URL to run real PostgreSQL tests")
	}

	ctx := t.Context()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}

	schema := fmt.Sprintf("pr_migration_test_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer admin.Close()
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	})

	parsedDSN, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsedDSN.Query()
	query.Set("search_path", schema)
	if poolSettings {
		query.Set("pool_max_conns", "2")
		query.Set("pool_max_conn_lifetime", "5m")
		query.Set("pool_health_check_period", "30s")
	}
	parsedDSN.RawQuery = query.Encode()
	scopedDSN := parsedDSN.String()

	t.Setenv("DATABASE_URL", scopedDSN)
	db, err := NewDBWithContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.CloseDB() }()
	pool := db.conn

	ddl, err := os.ReadFile(filepath.Join("..", "..", "..", "init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(ddl)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO teams(team_name) VALUES ('preserved')`); err != nil {
		t.Fatal(err)
	}

	if err := db.RunMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatalf("second migration run failed: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM teams WHERE team_name='preserved'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("preserved rows=%d, want 1", count)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM goose_db_version`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("migration version was not recorded")
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pr_reassign_idempotency`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("new idempotency table must be empty")
	}
}
