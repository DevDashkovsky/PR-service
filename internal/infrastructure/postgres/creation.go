package postgres

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct {
	conn *pgxpool.Pool
	dsn  string
}

func (db *Postgres) createConnectPath() (string, error) {
	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		config, err := pgxpool.ParseConfig(databaseURL)
		if err != nil {
			return "", fmt.Errorf("parse DATABASE_URL: %w", err)
		}
		if os.Getenv("ENVIRONMENT") == "production" && config.ConnConfig.Password == "change_me" {
			return "", fmt.Errorf("default database password is forbidden in production")
		}
		return databaseURL, nil
	}

	var params [5]string
	for i, key := range []string{"DB_USER", "DB_PASSWORD", "DB_HOST", "DB_PORT", "DB_NAME"} {
		params[i] = os.Getenv(key)
		if params[i] == "" {
			return "", fmt.Errorf("environment variable %s is not set", key)
		}
	}
	if os.Getenv("ENVIRONMENT") == "production" && params[1] == "change_me" {
		return "", fmt.Errorf("default database password is forbidden in production")
	}
	sslMode := os.Getenv("DB_SSLMODE")
	if sslMode == "" {
		sslMode = "disable"
	}
	query := url.Values{}
	query.Set("sslmode", sslMode)
	value := url.URL{Scheme: "postgres", User: url.UserPassword(params[0], params[1]), Host: net.JoinHostPort(params[2], params[3]), Path: "/" + params[4], RawPath: "/" + url.PathEscape(params[4]), RawQuery: query.Encode()}
	return value.String(), nil
}

func NewDB() (*Postgres, error) {
	return NewDBWithContext(context.Background())
}

func NewDBWithContext(parent context.Context) (*Postgres, error) {
	db := &Postgres{}
	path, err := db.createConnectPath()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	for {
		pingCtx, pingCancel := context.WithTimeout(ctx, 3*time.Second)
		err = pool.Ping(pingCtx)
		pingCancel()
		if err == nil {
			db.conn = pool
			db.dsn = path
			return db, nil
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			pool.Close()
			return nil, fmt.Errorf("connect database: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func (db *Postgres) Ping(ctx context.Context) error {
	return db.conn.Ping(ctx)
}

func (db *Postgres) CloseDB() error {
	if db.conn != nil {
		db.conn.Close()
	}
	return nil
}
