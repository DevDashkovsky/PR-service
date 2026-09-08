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
	var result *dto.ReassignPullRequestResponse
	err := s.prRepo.WithinTransaction(ctx, func(tx interfaces.PRTransaction) error {
		pullRequest, err := tx.GetPRForUpdate(ctx, req.PullRequestID)
		if err != nil {
			return err
		}
		if pullRequest.Status == dto.StatusMerged {
			return ErrPRMerged
		}
		if !slices.Contains(pullRequest.AssignedReviewers, req.OldUserID) {
			return ErrNotAssigned
		}
		oldReviewer, err := tx.GetUser(ctx, req.OldUserID)
		if errors.Is(err, user.ErrUserNotFound) {
			return ErrNotAssigned
		}
		if err != nil {
			return err
		}
		if oldReviewer.TeamName == "" {
			return ErrNoCandidate
		}
		team, err := tx.GetTeamForAssignment(ctx, oldReviewer.TeamName)
		if err != nil {
			return err
		}
		replacement, err := findReplacementCandidate(team.Members, pullRequest.AuthorID, pullRequest.AssignedReviewers, req.OldUserID)
		if err != nil {
			return err
		}
		if err := tx.ReplaceReviewer(ctx, req.PullRequestID, req.OldUserID, replacement); err != nil {
			return err
		}
		for i, id := range pullRequest.AssignedReviewers {
			if id == req.OldUserID {
				pullRequest.AssignedReviewers[i] = replacement
			}
		}
		slices.Sort(pullRequest.AssignedReviewers)
		result = &dto.ReassignPullRequestResponse{PR: *pullRequest, ReplacedBy: replacement}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
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
