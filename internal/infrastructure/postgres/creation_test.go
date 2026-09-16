package postgres

import (
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConnectionURLEscapesCredentialsAndDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("DB_SSLMODE", "verify-full")
	values := map[string]string{"DB_USER": "user@name", "DB_PASSWORD": "p@ss:/?#% word", "DB_HOST": "::1", "DB_PORT": "5432", "DB_NAME": "database/with?#% name"}
	for key, value := range values {
		t.Setenv(key, value)
	}
	path, err := (&Postgres{}).createConnectPath()
	if err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.User != values["DB_USER"] || config.ConnConfig.Password != values["DB_PASSWORD"] || config.ConnConfig.Database != values["DB_NAME"] || config.ConnConfig.Host != values["DB_HOST"] || parsed.Query().Get("sslmode") != "verify-full" {
		t.Fatal("connection URL changed a parameter")
	}
}

func TestConnectionURLConfiguration(t *testing.T) {
	t.Run("DATABASE_URL takes precedence", func(t *testing.T) {
		const databaseURL = "postgres://configured.example/database?sslmode=require"
		t.Setenv("DATABASE_URL", databaseURL)
		path, err := (&Postgres{}).createConnectPath()
		if err != nil || path != databaseURL {
			t.Fatalf("path=%q err=%v", path, err)
		}
	})

	t.Run("default password is rejected in production", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "")
		t.Setenv("ENVIRONMENT", "production")
		for key, value := range map[string]string{"DB_USER": "user", "DB_PASSWORD": "change_me", "DB_HOST": "db", "DB_PORT": "5432", "DB_NAME": "service"} {
			t.Setenv(key, value)
		}
		if _, err := (&Postgres{}).createConnectPath(); err == nil {
			t.Fatal("production accepted the default database password")
		}
	})

	t.Run("default password in DATABASE_URL is rejected in production", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://user:change_me@db:5432/service?sslmode=require")
		t.Setenv("ENVIRONMENT", "production")
		if _, err := (&Postgres{}).createConnectPath(); err == nil {
			t.Fatal("production DATABASE_URL accepted the default database password")
		}
	})
}
