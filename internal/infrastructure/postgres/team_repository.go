package postgres

import (
	"context"
	"errors"
	"fmt"
	"pr-service/internal/domain/dto"
	"pr-service/internal/domain/teams"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const uniqueViolationCode = "23505"

const (
	teamExistQuery  = `SELECT EXISTS(SELECT 1 FROM teams WHERE team_name = $1)`
	createTeamQuery = `INSERT INTO teams(team_name) VALUES ($1)`
	getTeamQuery    = `SELECT team_name FROM teams WHERE team_name = $1`
	getUsersQuery   = `SELECT user_id, username, is_active FROM users WHERE team_name = $1`
	upsertUserQuery = `
  INSERT INTO users (user_id, username, team_name, is_active)
  VALUES ($1, $2, $3, $4)
  ON CONFLICT (user_id) DO UPDATE
  SET username = EXCLUDED.username,
      team_name = EXCLUDED.team_name,
      is_active = EXCLUDED.is_active
 `
)

type TeamRepo struct {
	db *pgxpool.Pool
}

func NewTeamRepo(db *Postgres) *TeamRepo {
	return &TeamRepo{db: db.conn}
}

func (r *TeamRepo) TeamExists(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, teamExistQuery, name).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("ошибка при проверке существования команды: %w", err)
	}
	return exists, nil
}

func (r *TeamRepo) CreateTeam(ctx context.Context, team dto.TeamDTO) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("ошибка при начале транзакции: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, createTeamQuery, team.TeamName); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return teams.ErrTeamExists
		}
		return fmt.Errorf("ошибка при создании команды: %w", err)
	}

	for _, member := range team.Members {
		if _, err := tx.Exec(ctx, upsertUserQuery,
			member.UserID,
			member.Username,
			team.TeamName,
			member.IsActive,
		); err != nil {
			return fmt.Errorf("ошибка при добавлении пользователя %s: %w", member.UserID, err)
		}
	}

	return tx.Commit(ctx)
}

func (r *TeamRepo) GetTeam(ctx context.Context, name string) (dto.TeamDTO, error) {
	var teamName string
	err := r.db.QueryRow(ctx, getTeamQuery, name).Scan(&teamName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return dto.TeamDTO{}, teams.ErrTeamNotFound
		}
		return dto.TeamDTO{}, fmt.Errorf("ошибка при получении команды: %w", err)
	}

	rows, err := r.db.Query(ctx, getUsersQuery, teamName)
	if err != nil {
		return dto.TeamDTO{}, fmt.Errorf("ошибка при получении участников команды: %w", err)
	}
	defer rows.Close()

	var members []dto.TeamMemberDTO
	for rows.Next() {
		var member dto.TeamMemberDTO
		if err := rows.Scan(&member.UserID, &member.Username, &member.IsActive); err != nil {
			return dto.TeamDTO{}, fmt.Errorf("ошибка при чтении участников команды: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return dto.TeamDTO{}, fmt.Errorf("ошибка при итерации участников команды: %w", err)
	}

	return dto.TeamDTO{
		TeamName: teamName,
		Members:  members,
	}, nil
}
