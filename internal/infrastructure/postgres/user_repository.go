package postgres

import (
	"AvitoTech/internal/domain/dto"
	"AvitoTech/internal/domain/user"
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	getUserReviewsQuery = `
		SELECT pr.pull_request_id, pr.pull_request_name, pr.author_id, pr.status
		FROM pull_requests pr
		JOIN pr_reviewers prr ON pr.pull_request_id = prr.pull_request_id
		WHERE prr.reviewer_id = $1
		ORDER BY pr.created_at DESC
	`
	setUserActiveQuery = `UPDATE users SET is_active = $2 WHERE user_id = $1`
	getUserQuery       = `SELECT user_id, username, team_name, is_active FROM users WHERE user_id = $1`
	getTeamByNameQuery = `SELECT user_id, username, is_active FROM users WHERE team_name = $1`
)

type UserRepo struct {
	db *pgxpool.Pool
}

func NewUserRepo(db *Postgres) *UserRepo {
	return &UserRepo{db: db.conn}
}

func (r *UserRepo) GetUserReviews(ctx context.Context, userID string) ([]dto.PullRequestShortDTO, error) {
	rows, err := r.db.Query(ctx, getUserReviewsQuery, userID)
	if err != nil {
		return nil, fmt.Errorf("ошибка при получении ревью пользователя: %v", err)
	}
	defer rows.Close()

	reviews := []dto.PullRequestShortDTO{}
	for rows.Next() {
		var review dto.PullRequestShortDTO
		if err := rows.Scan(&review.PullRequestID, &review.PullRequestName, &review.AuthorID, &review.Status); err != nil {
			return nil, fmt.Errorf("ошибка при чтении ревью: %v", err)
		}
		reviews = append(reviews, review)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка при итерации ревью: %v", err)
	}

	return reviews, nil
}

func (r *UserRepo) SetUserActive(ctx context.Context, userID string, isActive bool) (*dto.UserDTO, error) {
	result, err := r.db.Exec(ctx, setUserActiveQuery, userID, isActive)
	if err != nil {
		return nil, fmt.Errorf("ошибка при обновлении статуса пользователя: %v", err)
	}
	if result.RowsAffected() == 0 {
		return nil, user.ErrUserNotFound
	}

	return r.GetUser(ctx, userID)
}

func (r *UserRepo) GetUser(ctx context.Context, userID string) (*dto.UserDTO, error) {
	var user dto.UserDTO
	err := r.db.QueryRow(ctx, getUserQuery, userID).Scan(&user.UserID, &user.Username, &user.TeamName, &user.IsActive)
	if err != nil {
		return nil, fmt.Errorf("ошибка при получении пользователя: %v", err)
	}
	return &user, nil
}

func (r *UserRepo) GetTeamByName(ctx context.Context, teamName string) (*dto.TeamDTO, error) {
	rows, err := r.db.Query(ctx, getTeamByNameQuery, teamName)
	if err != nil {
		return nil, fmt.Errorf("ошибка при получении команды: %v", err)
	}
	defer rows.Close()

	var members []dto.TeamMemberDTO
	for rows.Next() {
		var member dto.TeamMemberDTO
		if err := rows.Scan(&member.UserID, &member.Username, &member.IsActive); err != nil {
			return nil, fmt.Errorf("ошибка при чтении участника команды: %v", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка при итерации участников команды: %v", err)
	}

	return &dto.TeamDTO{
		TeamName: teamName,
		Members:  members,
	}, nil
}
