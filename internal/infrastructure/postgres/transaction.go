package postgres

import (
	"context"
	"errors"
	"pr-service/pkg/logger"
	"time"

	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		logger.Log.Warn("Transaction rollback failed", zap.Error(err))
	}
}
