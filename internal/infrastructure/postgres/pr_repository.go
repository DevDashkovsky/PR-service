package postgres

import (
	"AvitoTech/internal/domain/dto"
	"AvitoTech/internal/domain/pr"
	"context"
	"errors"
	"fmt"
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
		return false, fmt.Errorf("ошибка при проверке существования PR: %v", err)
	}
	return exists, nil
}

func (r *PRRepo) CreatePR(ctx context.Context, prID, prName, authorID string) error {
	result, err := r.db.Exec(ctx, createPRQuery, prID, prName, authorID, dto.StatusOpen, time.Now())
	if err != nil {
		return fmt.Errorf("ошибка при создании PR: %v", err)
	}
	if result.RowsAffected() == 0 {
		return pr.ErrPRExists
	}
	return nil
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
			return nil, fmt.Errorf("PR не найден")
		}
		return nil, fmt.Errorf("ошибка при получении PR: %v", err)
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
		return fmt.Errorf("ошибка при мердже PR: %v", err)
	}
	return nil
}

func (r *PRRepo) GetReviewers(ctx context.Context, prID string) ([]string, error) {
	rows, err := r.db.Query(ctx, getReviewersQuery, prID)
	if err != nil {
		return nil, fmt.Errorf("ошибка при получении ревьюверов: %v", err)
	}
	defer rows.Close()

	reviewers := []string{}
	for rows.Next() {
		var reviewerID string
		if err := rows.Scan(&reviewerID); err != nil {
			return nil, fmt.Errorf("ошибка при чтении ревьювера: %v", err)
		}
		reviewers = append(reviewers, reviewerID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка при итерации ревьюверов: %v", err)
	}

	return reviewers, nil
}

func (r *PRRepo) AssignReviewers(ctx context.Context, prID string, reviewerIDs []string) error {
	for _, reviewerID := range reviewerIDs {
		if _, err := r.db.Exec(ctx, assignReviewerQuery, prID, reviewerID); err != nil {
			return fmt.Errorf("ошибка при назначении ревьювера: %v", err)
		}
	}
	return nil
}

func (r *PRRepo) ReplaceReviewer(ctx context.Context, prID, oldReviewerID, newReviewerID string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("ошибка при начале транзакции: %v", err)
	}
	defer tx.Rollback(ctx)

	if _, err = tx.Exec(ctx, removeReviewerQuery, prID, oldReviewerID); err != nil {
		return fmt.Errorf("ошибка при удалении ревьювера: %v", err)
	}
	if _, err = tx.Exec(ctx, assignReviewerQuery, prID, newReviewerID); err != nil {
		return fmt.Errorf("ошибка при добавлении ревьювера: %v", err)
	}

	return tx.Commit(ctx)
}

func (r *PRRepo) IsReviewerAssigned(ctx context.Context, prID, reviewerID string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, isReviewerAssignedQuery, prID, reviewerID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("ошибка при проверке назначения ревьювера: %v", err)
	}
	return exists, nil
}
