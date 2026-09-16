package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"pr-service/internal/domain/dto"
	"pr-service/internal/domain/user"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	getUserReviewsQuery = `
		SELECT pr.pull_request_id, pr.pull_request_name, pr.author_id, pr.status, pr.created_at
		FROM pull_requests pr
		JOIN pr_reviewers prr ON pr.pull_request_id = prr.pull_request_id
		WHERE prr.reviewer_id = $1
		ORDER BY pr.created_at DESC, pr.pull_request_id COLLATE "C" DESC
		LIMIT $2
	`
	getUserReviewsAfterQuery = `
		SELECT pr.pull_request_id, pr.pull_request_name, pr.author_id, pr.status, pr.created_at
		FROM pull_requests pr
		JOIN pr_reviewers prr ON pr.pull_request_id = prr.pull_request_id
		WHERE prr.reviewer_id = $1
			AND (
				pr.created_at < $2
				OR (pr.created_at = $2 AND pr.pull_request_id COLLATE "C" < $3)
			)
		ORDER BY pr.created_at DESC, pr.pull_request_id COLLATE "C" DESC
		LIMIT $4
	`
	setUserActiveQuery = `UPDATE users SET is_active = $2 WHERE user_id = $1 RETURNING user_id, username, COALESCE(team_name, ''), is_active`
	getUserQuery       = `SELECT user_id, username, COALESCE(team_name, ''), COALESCE(is_active, false) FROM users WHERE user_id = $1`
)

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type UserRepo struct {
	db queryer
}

func NewUserRepo(db *Postgres) *UserRepo {
	return &UserRepo{db: db.conn}
}

func (r *UserRepo) GetUserReviews(
	ctx context.Context,
	userID string,
	page dto.UserReviewsPageRequest,
) (dto.UserReviewsPage, error) {
	query := getUserReviewsQuery
	args := []any{userID, page.Limit + 1}
	if page.Cursor != nil {
		query = getUserReviewsAfterQuery
		args = []any{userID, page.Cursor.CreatedAt, page.Cursor.PullRequestID, page.Limit + 1}
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return dto.UserReviewsPage{}, fmt.Errorf("ошибка при получении ревью пользователя: %w", err)
	}
	defer rows.Close()

	reviews := make([]dto.PullRequestShortDTO, 0, page.Limit)
	var boundary dto.UserReviewsCursor
	hasMore := false
	for rows.Next() {
		var review dto.PullRequestShortDTO
		var createdAt time.Time
		if err := rows.Scan(
			&review.PullRequestID,
			&review.PullRequestName,
			&review.AuthorID,
			&review.Status,
			&createdAt,
		); err != nil {
			return dto.UserReviewsPage{}, fmt.Errorf("ошибка при чтении ревью: %w", err)
		}
		if len(reviews) == page.Limit {
			hasMore = true
			continue
		}
		reviews = append(reviews, review)
		boundary = dto.UserReviewsCursor{
			CreatedAt:     createdAt,
			PullRequestID: review.PullRequestID,
		}
	}
	if err := rows.Err(); err != nil {
		return dto.UserReviewsPage{}, fmt.Errorf("ошибка при итерации ревью: %w", err)
	}

	result := dto.UserReviewsPage{PullRequests: reviews}
	if hasMore {
		result.NextCursor = &boundary
	}

	return result, nil
}

func (r *UserRepo) SetUserActive(ctx context.Context, userID string, isActive bool) (*dto.UserDTO, error) {
	var u dto.UserDTO
	err := r.db.QueryRow(ctx, setUserActiveQuery, userID, isActive).Scan(&u.UserID, &u.Username, &u.TeamName, &u.IsActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, user.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update user activity: %w", err)
	}
	return &u, nil
}

func (r *UserRepo) GetUser(ctx context.Context, userID string) (*dto.UserDTO, error) {
	var u dto.UserDTO
	err := r.db.QueryRow(ctx, getUserQuery, userID).Scan(&u.UserID, &u.Username, &u.TeamName, &u.IsActive)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, user.ErrUserNotFound
		}
		return nil, fmt.Errorf("ошибка при получении пользователя: %w", err)
	}
	return &u, nil
}
