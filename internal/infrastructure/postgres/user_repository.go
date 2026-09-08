package postgres

import (
	"context"
	"errors"
	"fmt"
	"pr-service/internal/domain/dto"
	"pr-service/internal/domain/user"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	getUserReviewsQuery = `
		SELECT pr.pull_request_id, pr.pull_request_name, pr.author_id, pr.status
		FROM pull_requests pr
		JOIN pr_reviewers prr ON pr.pull_request_id = prr.pull_request_id
		WHERE prr.reviewer_id = $1
		ORDER BY pr.created_at DESC, pr.pull_request_id
	`
	setUserActiveQuery = `UPDATE users SET is_active = $2 WHERE user_id = $1 RETURNING user_id, username, COALESCE(team_name, ''), is_active`
	getUserQuery       = `SELECT user_id, username, COALESCE(team_name, ''), COALESCE(is_active, false) FROM users WHERE user_id = $1`
	getTeamByNameQuery = `SELECT user_id, username, COALESCE(is_active, false) FROM users WHERE team_name = $1 ORDER BY user_id`
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

func (r *UserRepo) GetUserReviews(ctx context.Context, userID string) ([]dto.PullRequestShortDTO, error) {
	rows, err := r.db.Query(ctx, getUserReviewsQuery, userID)
	if err != nil {
		return nil, fmt.Errorf("ошибка при получении ревью пользователя: %w", err)
	}
	defer rows.Close()

	reviews := []dto.PullRequestShortDTO{}
	for rows.Next() {
		var review dto.PullRequestShortDTO
		if err := rows.Scan(&review.PullRequestID, &review.PullRequestName, &review.AuthorID, &review.Status); err != nil {
			return nil, fmt.Errorf("ошибка при чтении ревью: %w", err)
		}
		reviews = append(reviews, review)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка при итерации ревью: %w", err)
	}

	return reviews, nil
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

func (r *UserRepo) GetTeamByName(ctx context.Context, teamName string) (*dto.TeamDTO, error) {
	rows, err := r.db.Query(ctx, getTeamByNameQuery, teamName)
	if err != nil {
		return nil, fmt.Errorf("ошибка при получении команды: %w", err)
	}
	defer rows.Close()

	members := []dto.TeamMemberDTO{}
	for rows.Next() {
		var member dto.TeamMemberDTO
		if err := rows.Scan(&member.UserID, &member.Username, &member.IsActive); err != nil {
			return nil, fmt.Errorf("ошибка при чтении участника команды: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка при итерации участников команды: %w", err)
	}

	return &dto.TeamDTO{
		TeamName: teamName,
		Members:  members,
	}, nil
}
