package pr

import (
	"pr-service/internal/domain/dto"
	"pr-service/internal/domain/user"
	"pr-service/pkg/logger"
	"pr-service/pkg/validator"
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"
)

func (s *Service) CreatePR(ctx context.Context, req dto.CreatePullRequestRequest) (*dto.PullRequestDTO, error) {
	if err := validator.ValidatePullRequestID(req.PullRequestID); err != nil {
		return nil, fmt.Errorf("invalid pull_request_id: %w", err)
	}
	if err := validator.ValidateUsername(req.PullRequestName); err != nil {
		return nil, fmt.Errorf("invalid pull_request_name: %w", err)
	}
	if err := validator.ValidateUserID(req.AuthorID); err != nil {
		return nil, fmt.Errorf("invalid author_id: %w", err)
	}

	logger.Log.Info("Создание PR",
		zap.String("pr_id", req.PullRequestID),
		zap.String("pr_name", req.PullRequestName),
		zap.String("author_id", req.AuthorID),
	)

	exists, err := s.prRepo.PRExists(ctx, req.PullRequestID)
	if err != nil {
		return nil, fmt.Errorf("ошибка при проверке существования PR: %w", err)
	}
	if exists {
		logger.Log.Warn("PR уже существует", zap.String("pr_id", req.PullRequestID))
		return nil, ErrPRExists
	}

	author, err := s.userRepo.GetUser(ctx, req.AuthorID)
	if err != nil {
		if errors.Is(err, user.ErrUserNotFound) {
			logger.Log.Warn("Автор не найден", zap.String("author_id", req.AuthorID))
			return nil, ErrAuthorNotFound
		}
		logger.Log.Error("Ошибка при получении автора", zap.String("author_id", req.AuthorID), zap.Error(err))
		return nil, fmt.Errorf("ошибка при получении автора: %w", err)
	}

	if author.TeamName == "" {
		logger.Log.Warn("У автора нет команды", zap.String("author_id", req.AuthorID))
		return nil, ErrAuthorNoTeam
	}

	reviewers, err := s.assignReviewers(ctx, req.AuthorID, author.TeamName)
	if err != nil {
		logger.Log.Error("Ошибка при автоназначении ревьюверов", zap.Error(err))
		return nil, fmt.Errorf("ошибка при назначении ревьюверов: %w", err)
	}

	if err := s.prRepo.CreatePRWithReviewers(ctx, req.PullRequestID, req.PullRequestName, req.AuthorID, reviewers); err != nil {
		logger.Log.Error("Ошибка при создании PR в БД", zap.Error(err))
		return nil, fmt.Errorf("ошибка при создании PR: %w", err)
	}

	logger.Log.Info("PR успешно создан",
		zap.String("pr_id", req.PullRequestID),
		zap.Int("reviewers_count", len(reviewers)),
		zap.Strings("reviewers", reviewers),
	)

	return &dto.PullRequestDTO{
		PullRequestID:     req.PullRequestID,
		PullRequestName:   req.PullRequestName,
		AuthorID:          req.AuthorID,
		Status:            dto.StatusOpen,
		AssignedReviewers: reviewers,
	}, nil
}
