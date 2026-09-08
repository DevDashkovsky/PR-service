package interfaces

import (
	"context"
	"pr-service/internal/domain/dto"
)

type TeamRepository interface {
	CreateTeam(ctx context.Context, team dto.TeamDTO) error
	TeamExists(ctx context.Context, name string) (bool, error)
	GetTeam(ctx context.Context, name string) (dto.TeamDTO, error)
}
