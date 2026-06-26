package pr

import (
	"pr-service/internal/domain/interfaces"
)

type Service struct {
	prRepo   interfaces.PRRepository
	userRepo interfaces.UserRepository
}

func NewService(prRepo interfaces.PRRepository, userRepo interfaces.UserRepository) *Service {
	return &Service{
		prRepo:   prRepo,
		userRepo: userRepo,
	}
}
