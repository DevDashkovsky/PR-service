package postgres

import (
	"AvitoTech/pkg/logger"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type Postgres struct {
	conn *pgxpool.Pool
}

func (db *Postgres) createConnectPath() (string, error) {
	var dbParam [5]string
	for i, param := range []string{"DB_USER", "DB_PASSWORD", "DB_HOST", "DB_PORT", "DB_NAME"} {
		value := os.Getenv(param)
		if value == "" {
			return "", fmt.Errorf("environment variable %s is not set", param)
		}
		dbParam[i] = value
	}

	dbURL := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		dbParam[0],
		dbParam[1],
		dbParam[2],
		dbParam[3],
		dbParam[4],
	)

	return dbURL, nil
}

func NewDB() (*Postgres, error) {
	db := &Postgres{}
	path, err := db.createConnectPath()
	if err != nil {
		return db, err
	}

	logger.Log.Info("Подключение к PostgreSQL...",
		zap.String("host", os.Getenv("DB_HOST")),
		zap.String("port", os.Getenv("DB_PORT")),
		zap.String("database", os.Getenv("DB_NAME")),
	)

	const maxAttempts = 10
	const retryDelay = 2 * time.Second

	var pool *pgxpool.Pool
	for i := range maxAttempts {
		pool, err = pgxpool.New(context.Background(), path)
		if err == nil {
			if pingErr := pool.Ping(context.Background()); pingErr == nil {
				break
			} else {
				pool.Close()
				err = pingErr
			}
		}
		logger.Log.Warn("БД недоступна, повтор...",
			zap.Int("attempt", i+1),
			zap.Int("max", maxAttempts),
			zap.Error(err),
		)
		time.Sleep(retryDelay)
	}
	if err != nil {
		return db, fmt.Errorf("ошибка подключения к базе данных: %w", err)
	}

	db.conn = pool
	logger.Log.Info("Успешное подключение к PostgreSQL")

	return db, nil
}

func (db *Postgres) CloseDB() error {
	db.conn.Close()
	return nil
}

func (db *Postgres) GetConn() *pgxpool.Pool {
	return db.conn
}
