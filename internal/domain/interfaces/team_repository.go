package interfaces

import (
	"pr-service/internal/domain/dto"
	"context"
)

type TeamRepository interface {
	CreateTeam(ctx context.Context, team dto.TeamDTO) error
	TeamExists(ctx context.Context, name string) (bool, error)
	GetTeam(ctx context.Context, name string) (dto.TeamDTO, error)
}
