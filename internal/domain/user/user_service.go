package user

import (
	"context"
	"errors"
	"fmt"
	"pr-service/internal/domain/dto"
	"pr-service/internal/domain/interfaces"
	"pr-service/pkg/validator"
)

const (
	BadRequest    = "BAD_REQUEST"
	InternalError = "INTERNAL_ERROR"
	UserNotFound  = "NOT_FOUND"
)

var (
	ErrUserNotFound = errors.New("пользователь не найден")
)

type Service struct {
	repo interfaces.UserRepository
}

func NewService(repo interfaces.UserRepository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetUserReviews(ctx context.Context, userID string) ([]dto.PullRequestShortDTO, error) {
	if err := validator.ValidateUserID(userID); err != nil {
		return nil, err
	}
	reviews, err := s.repo.GetUserReviews(ctx, userID)
	if err != nil {
		return nil, err
	}

	return reviews, nil
}

func (s *Service) SetUserActive(ctx context.Context, userID string, isActive bool) (*dto.UserDTO, error) {
	if err := validator.ValidateUserID(userID); err != nil {
		return nil, fmt.Errorf("invalid user_id: %w", err)
	}

	user, err := s.repo.SetUserActive(ctx, userID, isActive)
	if err != nil {
		return nil, err
	}

	return user, nil
}
