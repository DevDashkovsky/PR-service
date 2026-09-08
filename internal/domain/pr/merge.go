package pr

import (
	"context"
	"pr-service/internal/domain/dto"
	"pr-service/internal/domain/interfaces"
	"pr-service/pkg/validator"
	"time"
)

func (s *Service) MergePR(ctx context.Context, req dto.MergePullRequestRequest) (*dto.MergedPullRequestDTO, error) {
	if err := validator.ValidatePullRequestID(req.PullRequestID); err != nil {
		return nil, err
	}
	var result *dto.MergedPullRequestDTO
	err := s.prRepo.WithinTransaction(ctx, func(tx interfaces.PRTransaction) error {
		pullRequest, err := tx.GetPRForUpdate(ctx, req.PullRequestID)
		if err != nil {
			return err
		}
		if pullRequest.Status != dto.StatusMerged {
			now := time.Now().UTC().Truncate(time.Microsecond)
			if err := tx.SetMerged(ctx, req.PullRequestID, now); err != nil {
				return err
			}
			timestamp := now.Format(time.RFC3339Nano)
			pullRequest.MergedAt = &timestamp
		}
		mergedAt := ""
		if pullRequest.MergedAt != nil {
			mergedAt = *pullRequest.MergedAt
		}
		result = &dto.MergedPullRequestDTO{PullRequestID: pullRequest.PullRequestID, PullRequestName: pullRequest.PullRequestName, AuthorID: pullRequest.AuthorID, Status: dto.StatusMerged, AssignedReviewers: pullRequest.AssignedReviewers, MergedAt: mergedAt}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
