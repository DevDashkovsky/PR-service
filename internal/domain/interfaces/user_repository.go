package interfaces

import (
	"context"
	"pr-service/internal/domain/dto"
)

type UserRepository interface {
	GetUserReviews(ctx context.Context, userID string, page dto.UserReviewsPageRequest) (dto.UserReviewsPage, error)
	SetUserActive(ctx context.Context, userID string, isActive bool) (*dto.UserDTO, error)
	GetUser(ctx context.Context, userID string) (*dto.UserDTO, error)
}
