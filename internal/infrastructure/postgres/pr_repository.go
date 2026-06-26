package postgres

import (
	"context"
	"errors"
	"fmt"
	"pr-service/internal/domain/dto"
	prdomain "pr-service/internal/domain/pr"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	prExistsQuery = `SELECT EXISTS(SELECT 1 FROM pull_requests WHERE pull_request_id = $1)`

	createPRQuery = `
		INSERT INTO pull_requests (pull_request_id, pull_request_name, author_id, status, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (pull_request_id) DO NOTHING
	`

	getPRQuery = `
		SELECT pull_request_id, pull_request_name, author_id, status, merged_at
		FROM pull_requests
		WHERE pull_request_id = $1
	`

	setMergedQuery = `
		UPDATE pull_requests
		SET status = 'MERGED', merged_at = $2
		WHERE pull_request_id = $1
	`

	getReviewersQuery = `
		SELECT reviewer_id
		FROM pr_reviewers
		WHERE pull_request_id = $1
		ORDER BY assigned_at
	`

	assignReviewerQuery = `
		INSERT INTO pr_reviewers (pull_request_id, reviewer_id)
		VALUES ($1, $2)
	`

	removeReviewerQuery = `
		DELETE FROM pr_reviewers
		WHERE pull_request_id = $1 AND reviewer_id = $2
	`

	isReviewerAssignedQuery = `
		SELECT EXISTS(
			SELECT 1 FROM pr_reviewers
			WHERE pull_request_id = $1 AND reviewer_id = $2
		)
	`
)

type PRRepo struct {
	db *pgxpool.Pool
}

func NewPRRepo(db *Postgres) *PRRepo {
	return &PRRepo{db: db.conn}
}

func (r *PRRepo) PRExists(ctx context.Context, prID string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, prExistsQuery, prID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("ошибка при проверке существования PR: %w", err)
	}
	return exists, nil
}

func (r *PRRepo) CreatePRWithReviewers(ctx context.Context, prID, prName, authorID string, reviewerIDs []string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("ошибка при начале транзакции: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result, err := tx.Exec(ctx, createPRQuery, prID, prName, authorID, dto.StatusOpen, time.Now())
	if err != nil {
		return fmt.Errorf("ошибка при создании PR: %w", err)
	}
	if result.RowsAffected() == 0 {
		return prdomain.ErrPRExists
	}

	if len(reviewerIDs) > 0 {
		batch := &pgx.Batch{}
		for _, reviewerID := range reviewerIDs {
			batch.Queue(assignReviewerQuery, prID, reviewerID)
		}
		br := tx.SendBatch(ctx, batch)
		for range reviewerIDs {
			if _, err := br.Exec(); err != nil {
				_ = br.Close()
				return fmt.Errorf("ошибка при назначении ревьювера: %w", err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("ошибка при назначении ревьюверов: %w", err)
		}
	}

	return tx.Commit(ctx)
}

func (r *PRRepo) GetPR(ctx context.Context, prID string) (*dto.PullRequestDTO, error) {
	var pr dto.PullRequestDTO
	var mergedAt pgtype.Timestamptz
	err := r.db.QueryRow(ctx, getPRQuery, prID).Scan(
		&pr.PullRequestID,
		&pr.PullRequestName,
		&pr.AuthorID,
		&pr.Status,
		&mergedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, prdomain.ErrPRNotFound
		}
		return nil, fmt.Errorf("ошибка при получении PR: %w", err)
	}

	if mergedAt.Valid {
		s := mergedAt.Time.Format(time.RFC3339)
		pr.MergedAt = &s
	}

	reviewers, err := r.GetReviewers(ctx, prID)
	if err != nil {
		return nil, err
	}
	pr.AssignedReviewers = reviewers

	return &pr, nil
}

func (r *PRRepo) SetMerged(ctx context.Context, prID string, mergedAt time.Time) error {
	_, err := r.db.Exec(ctx, setMergedQuery, prID, mergedAt)
	if err != nil {
		return fmt.Errorf("ошибка при мердже PR: %w", err)
	}
	return nil
}

func (r *PRRepo) GetReviewers(ctx context.Context, prID string) ([]string, error) {
	rows, err := r.db.Query(ctx, getReviewersQuery, prID)
	if err != nil {
		return nil, fmt.Errorf("ошибка при получении ревьюверов: %w", err)
	}
	defer rows.Close()

	reviewers := []string{}
	for rows.Next() {
		var reviewerID string
		if err := rows.Scan(&reviewerID); err != nil {
			return nil, fmt.Errorf("ошибка при чтении ревьювера: %w", err)
		}
		reviewers = append(reviewers, reviewerID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка при итерации ревьюверов: %w", err)
	}

	return reviewers, nil
}

func (r *PRRepo) ReplaceReviewer(ctx context.Context, prID, oldReviewerID, newReviewerID string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("ошибка при начале транзакции: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err = tx.Exec(ctx, removeReviewerQuery, prID, oldReviewerID); err != nil {
		return fmt.Errorf("ошибка при удалении ревьювера: %w", err)
	}
	if _, err = tx.Exec(ctx, assignReviewerQuery, prID, newReviewerID); err != nil {
		return fmt.Errorf("ошибка при добавлении ревьювера: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *PRRepo) IsReviewerAssigned(ctx context.Context, prID, reviewerID string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, isReviewerAssignedQuery, prID, reviewerID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("ошибка при проверке назначения ревьювера: %w", err)
	}
	return exists, nil
}
