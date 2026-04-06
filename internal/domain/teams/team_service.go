package teams

import (
	"AvitoTech/internal/domain/dto"
	"AvitoTech/internal/domain/interfaces"
	"AvitoTech/pkg/validator"
	"context"
	"errors"
	"fmt"
)

const (
	TeamExistsCode = "TEAM_EXISTS"
	BadRequest     = "BAD_REQUEST"
	InternalError  = "INTERNAL_ERROR"
	TeamNotFound   = "NOT_FOUND"
)

var (
	ErrTeamExists   = errors.New("teams already exists")
	ErrTeamNotFound = errors.New("team not found")
)

type Service struct {
	teams interfaces.TeamRepository
	users interfaces.UserRepository
}

func NewService(t interfaces.TeamRepository, u interfaces.UserRepository) *Service {
	return &Service{teams: t, users: u}
}

func (s *Service) CreateTeam(ctx context.Context, req dto.TeamDTO) error {
	if err := validator.ValidateTeamName(req.TeamName); err != nil {
		return fmt.Errorf("invalid team_name: %w", err)
	}

	if len(req.Members) == 0 {
		return errors.New("members list cannot be empty")
	}

	if len(req.Members) > 200 {
		return errors.New("too many members (max 200)")
	}

	userIDs := make([]string, 0, len(req.Members))
	for _, member := range req.Members {
		if err := validator.ValidateUserID(member.UserID); err != nil {
			return fmt.Errorf("invalid user_id '%s': %w", member.UserID, err)
		}
		if err := validator.ValidateUsername(member.Username); err != nil {
			return fmt.Errorf("invalid username for user '%s': %w", member.UserID, err)
		}
		userIDs = append(userIDs, member.UserID)
	}

	if err := validator.ValidateMembersUnique(userIDs); err != nil {
		return err
	}

	exists, err := s.teams.TeamExists(ctx, req.TeamName)
	if err != nil {
		return fmt.Errorf("ошибка при проверке существования команды: %v", err)
	}
	if exists {
		return ErrTeamExists
	}

	err = s.teams.CreateTeam(ctx, req)
	if err != nil {
		return fmt.Errorf("ошибка при создании команды: %v", err)
	}

	return nil
}

func (s *Service) GetTeam(ctx context.Context, teamName string) (dto.TeamDTO, error) {
	return s.teams.GetTeam(ctx, teamName)
}
