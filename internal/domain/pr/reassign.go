package pr

import (
	"AvitoTech/internal/domain/dto"
	"AvitoTech/pkg/logger"
	"AvitoTech/pkg/validator"
	"context"
	"fmt"

	"go.uber.org/zap"
)

func (s *Service) ReassignReviewer(ctx context.Context, req dto.ReassignPullRequestRequest) (*dto.ReassignPullRequestResponse, error) {
	if err := validator.ValidateUserID(req.PullRequestID); err != nil {
		return nil, fmt.Errorf("invalid pull_request_id: %w", err)
	}
	if err := validator.ValidateUserID(req.OldUserID); err != nil {
		return nil, fmt.Errorf("invalid old_user_id: %w", err)
	}

	logger.Log.Info("Переназначение ревьювера",
		zap.String("pr_id", req.PullRequestID),
		zap.String("old_user_id", req.OldUserID),
	)

	pr, err := s.prRepo.GetPR(ctx, req.PullRequestID)
	if err != nil {
		logger.Log.Error("PR не найден", zap.String("pr_id", req.PullRequestID), zap.Error(err))
		return nil, ErrPRNotFound
	}

	if pr.Status == dto.StatusMerged {
		logger.Log.Warn("Попытка переназначить ревьювера на смердженный PR",
			zap.String("pr_id", req.PullRequestID),
		)
		return nil, ErrPRMerged
	}

	isAssigned, err := s.prRepo.IsReviewerAssigned(ctx, req.PullRequestID, req.OldUserID)
	if err != nil {
		return nil, fmt.Errorf("ошибка при проверке назначения: %w", err)
	}
	if !isAssigned {
		logger.Log.Warn("Пользователь не назначен ревьювером на этот PR",
			zap.String("pr_id", req.PullRequestID),
			zap.String("user_id", req.OldUserID),
		)
		return nil, ErrNotAssigned
	}

	oldReviewer, err := s.userRepo.GetUser(ctx, req.OldUserID)
	if err != nil {
		logger.Log.Error("Старый ревьювер не найден", zap.String("user_id", req.OldUserID), zap.Error(err))
		return nil, fmt.Errorf("old reviewer not found: %w", err)
	}

	if oldReviewer.TeamName == "" {
		return nil, fmt.Errorf("old reviewer has no team")
	}

	newReviewer, err := s.findReplacementCandidate(ctx, oldReviewer.TeamName, pr.AuthorID, pr.AssignedReviewers, req.OldUserID)
	if err != nil {
		logger.Log.Warn("Не найден кандидат для замены",
			zap.String("team_name", oldReviewer.TeamName),
			zap.Error(err),
		)
		return nil, err
	}

	if err := s.prRepo.ReplaceReviewer(ctx, req.PullRequestID, req.OldUserID, newReviewer); err != nil {
		logger.Log.Error("Ошибка при переназначении ревьювера", zap.Error(err))
		return nil, fmt.Errorf("ошибка при переназначении ревьювера: %w", err)
	}

	logger.Log.Info("Ревьювер успешно переназначен",
		zap.String("pr_id", req.PullRequestID),
		zap.String("old_reviewer", req.OldUserID),
		zap.String("new_reviewer", newReviewer),
	)

	updatedPR, err := s.prRepo.GetPR(ctx, req.PullRequestID)
	if err != nil {
		return nil, fmt.Errorf("ошибка при получении обновленного PR: %w", err)
	}

	return &dto.ReassignPullRequestResponse{
		PR: dto.PullRequestDTO{
			PullRequestID:     updatedPR.PullRequestID,
			PullRequestName:   updatedPR.PullRequestName,
			AuthorID:          updatedPR.AuthorID,
			Status:            updatedPR.Status,
			AssignedReviewers: updatedPR.AssignedReviewers,
		},
		ReplacedBy: newReviewer,
	}, nil
}

func (s *Service) findReplacementCandidate(ctx context.Context, teamName, authorID string, currentReviewers []string, oldReviewerID string) (string, error) {
	team, err := s.userRepo.GetTeamByName(ctx, teamName)
	if err != nil {
		return "", fmt.Errorf("ошибка при получении команды: %w", err)
	}

	excludeMap := make(map[string]bool)
	excludeMap[authorID] = true
	excludeMap[oldReviewerID] = true
	for _, reviewerID := range currentReviewers {
		excludeMap[reviewerID] = true
	}

	var candidates []string
	for _, member := range team.Members {
		if member.IsActive && !excludeMap[member.UserID] {
			candidates = append(candidates, member.UserID)
		}
	}

	if len(candidates) == 0 {
		return "", ErrNoCandidate
	}

	selected := selectRandomReviewers(candidates, 1)
	return selected[0], nil
}
