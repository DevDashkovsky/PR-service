package pr

import (
	"context"
	"errors"
	"pr-service/internal/domain/dto"
	"pr-service/internal/domain/interfaces"
	"pr-service/internal/domain/user"
	"pr-service/pkg/validator"
	"slices"
)

func (s *Service) ReassignReviewer(ctx context.Context, req dto.ReassignPullRequestRequest) (*dto.ReassignPullRequestResponse, error) {
	if err := validator.ValidatePullRequestID(req.PullRequestID); err != nil {
		return nil, err
	}
	if err := validator.ValidateUserID(req.OldUserID); err != nil {
		return nil, err
	}
	if err := validator.ValidateIdempotencyKey(req.IdempotencyKey); err != nil {
		return nil, err
	}
	var result *dto.ReassignPullRequestResponse
	err := s.prRepo.WithinTransaction(ctx, func(tx interfaces.PRTransaction) error {
		if req.IdempotencyKey != "" {
			cached, err := tx.BeginReassignment(ctx, req.IdempotencyKey, req.PullRequestID, req.OldUserID)
			if err != nil {
				return err
			}
			if cached != nil {
				result = cached
				return nil
			}
		}
		updated, err := performReassignment(ctx, tx, req)
		if err != nil {
			return err
		}
		result = updated
		if req.IdempotencyKey != "" {
			return tx.SaveReassignment(ctx, req.IdempotencyKey, result)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func performReassignment(ctx context.Context, tx interfaces.PRTransaction, req dto.ReassignPullRequestRequest) (*dto.ReassignPullRequestResponse, error) {
	pullRequest, err := tx.GetPRForUpdate(ctx, req.PullRequestID)
	if err != nil {
		return nil, err
	}
	if pullRequest.Status == dto.StatusMerged {
		return nil, ErrPRMerged
	}
	if !slices.Contains(pullRequest.AssignedReviewers, req.OldUserID) {
		return nil, ErrNotAssigned
	}
	oldReviewer, err := tx.GetUser(ctx, req.OldUserID)
	if errors.Is(err, user.ErrUserNotFound) {
		return nil, ErrNotAssigned
	}
	if err != nil {
		return nil, err
	}
	if oldReviewer.TeamName == "" {
		return nil, ErrNoCandidate
	}
	team, err := tx.GetTeamForAssignment(ctx, oldReviewer.TeamName)
	if err != nil {
		return nil, err
	}
	replacement, err := findReplacementCandidate(team.Members, pullRequest.AuthorID, pullRequest.AssignedReviewers, req.OldUserID)
	if err != nil {
		return nil, err
	}
	if err := tx.ReplaceReviewer(ctx, req.PullRequestID, req.OldUserID, replacement); err != nil {
		return nil, err
	}
	for i, id := range pullRequest.AssignedReviewers {
		if id == req.OldUserID {
			pullRequest.AssignedReviewers[i] = replacement
		}
	}
	slices.Sort(pullRequest.AssignedReviewers)
	return &dto.ReassignPullRequestResponse{PR: *pullRequest, ReplacedBy: replacement}, nil
}

func findReplacementCandidate(members []dto.TeamMemberDTO, authorID string, currentReviewers []string, oldReviewerID string) (string, error) {
	candidates := make([]string, 0, len(members))
	for _, member := range members {
		if member.IsActive && member.UserID != authorID && member.UserID != oldReviewerID && !slices.Contains(currentReviewers, member.UserID) {
			candidates = append(candidates, member.UserID)
		}
	}
	if len(candidates) == 0 {
		return "", ErrNoCandidate
	}
	return selectRandomReviewers(candidates, 1)[0], nil
}
