package pr

import (
	"pr-service/internal/domain/dto"
	"pr-service/pkg/logger"
	"pr-service/pkg/validator"
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
)

func (s *Service) MergePR(ctx context.Context, req dto.MergePullRequestRequest) (*dto.MergedPullRequestDTO, error) {
	if err := validator.ValidatePullRequestID(req.PullRequestID); err != nil {
		return nil, fmt.Errorf("invalid pull_request_id: %w", err)
	}

	logger.Log.Info("Мердж PR", zap.String("pr_id", req.PullRequestID))

	pr, err := s.prRepo.GetPR(ctx, req.PullRequestID)
	if err != nil {
		if errors.Is(err, ErrPRNotFound) {
			logger.Log.Warn("PR не найден", zap.String("pr_id", req.PullRequestID))
			return nil, ErrPRNotFound
		}
		logger.Log.Error("Ошибка при получении PR", zap.String("pr_id", req.PullRequestID), zap.Error(err))
		return nil, fmt.Errorf("ошибка при получении PR: %w", err)
	}

	if pr.Status == dto.StatusMerged {
		logger.Log.Info("PR уже смерджен", zap.String("pr_id", req.PullRequestID))
		mergedAt := ""
		if pr.MergedAt != nil {
			mergedAt = *pr.MergedAt
		}
		return &dto.MergedPullRequestDTO{
			PullRequestID:     pr.PullRequestID,
			PullRequestName:   pr.PullRequestName,
			AuthorID:          pr.AuthorID,
			Status:            dto.StatusMerged,
			AssignedReviewers: pr.AssignedReviewers,
			MergedAt:          mergedAt,
		}, nil
	}

	now := time.Now()
	nowStr := now.Format(time.RFC3339)

	if err := s.prRepo.SetMerged(ctx, req.PullRequestID, now); err != nil {
		logger.Log.Error("Ошибка при мердже PR", zap.Error(err))
		return nil, fmt.Errorf("ошибка при мердже PR: %w", err)
	}

	logger.Log.Info("PR успешно смерджен",
		zap.String("pr_id", req.PullRequestID),
		zap.String("merged_at", nowStr),
	)

	return &dto.MergedPullRequestDTO{
		PullRequestID:     pr.PullRequestID,
		PullRequestName:   pr.PullRequestName,
		AuthorID:          pr.AuthorID,
		Status:            dto.StatusMerged,
		AssignedReviewers: pr.AssignedReviewers,
		MergedAt:          nowStr,
	}, nil
}
