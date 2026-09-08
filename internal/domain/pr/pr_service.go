package pr

import "pr-service/internal/domain/interfaces"

type Service struct{ prRepo interfaces.PRRepository }

func NewService(prRepo interfaces.PRRepository) *Service { return &Service{prRepo: prRepo} }
