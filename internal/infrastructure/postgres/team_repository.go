package postgres

import (
	"AvitoTech/internal/domain/dto"
	"AvitoTech/internal/domain/teams"
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
		return false, fmt.Errorf("ошибка при проверке существования команды: %v", err)
	}
	return exists, nil
}

func (r *TeamRepo) CreateTeam(ctx context.Context, team dto.TeamDTO) error {
	_, err := r.db.Exec(ctx, createTeamQuery, team.TeamName)
	if err != nil {
		return fmt.Errorf("ошибка при создании команды: %v", err)
	}

	for _, member := range team.Members {
		_, err := r.db.Exec(ctx, upsertUserQuery,
			member.UserID,
			member.Username,
			team.TeamName,
			member.IsActive,
		)
		if err != nil {
			return fmt.Errorf("ошибка при добавлении пользователя %s: %v", member.UserID, err)
		}
	}

	return nil
}

func (r *TeamRepo) GetTeam(ctx context.Context, name string) (dto.TeamDTO, error) {
	var teamName string
	err := r.db.QueryRow(ctx, getTeamQuery, name).Scan(&teamName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return dto.TeamDTO{}, teams.ErrTeamNotFound
		}
		return dto.TeamDTO{}, fmt.Errorf("ошибка при получении команды: %v", err)
	}

	rows, err := r.db.Query(ctx, getUsersQuery, teamName)
	if err != nil {
		return dto.TeamDTO{}, fmt.Errorf("ошибка при получении участников команды: %v", err)
	}
	defer rows.Close()

	var members []dto.TeamMemberDTO
	for rows.Next() {
		var member dto.TeamMemberDTO
		if err := rows.Scan(&member.UserID, &member.Username, &member.IsActive); err != nil {
			return dto.TeamDTO{}, fmt.Errorf("ошибка при чтении участников команды: %v", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return dto.TeamDTO{}, fmt.Errorf("ошибка при итерации участников команды: %v", err)
	}

	return dto.TeamDTO{
		TeamName: teamName,
		Members:  members,
	}, nil
}
