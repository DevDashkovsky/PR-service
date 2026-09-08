package pr

import (
	"context"
	"errors"
	"fmt"
	"pr-service/internal/domain/dto"
	"pr-service/internal/domain/interfaces"
	"pr-service/internal/domain/user"
	"pr-service/pkg/validator"
)

func (s *Service) CreatePR(ctx context.Context, req dto.CreatePullRequestRequest) (*dto.PullRequestDTO, error) {
	if err := validator.ValidatePullRequestID(req.PullRequestID); err != nil {
		return nil, err
	}
	if err := validator.ValidateUsername(req.PullRequestName); err != nil {
		return nil, fmt.Errorf("invalid pull_request_name: %w", err)
	}
	if err := validator.ValidateUserID(req.AuthorID); err != nil {
		return nil, err
	}
	var result *dto.PullRequestDTO
	err := s.prRepo.WithinTransaction(ctx, func(tx interfaces.PRTransaction) error {
		exists, err := tx.PRExists(ctx, req.PullRequestID)
		if err != nil {
			return err
		}
		if exists {
			return ErrPRExists
		}
		author, err := tx.GetUser(ctx, req.AuthorID)
		if errors.Is(err, user.ErrUserNotFound) {
			return ErrAuthorNotFound
		}
		if err != nil {
			return err
		}
		if author.TeamName == "" {
			return ErrAuthorNoTeam
		}
		team, err := tx.GetTeamForAssignment(ctx, author.TeamName)
		if err != nil {
			return err
		}
		reviewers := assignReviewers(team.Members, req.AuthorID)
		if err := tx.CreatePRWithReviewers(ctx, req.PullRequestID, req.PullRequestName, req.AuthorID, reviewers); err != nil {
			return err
		}
		result = &dto.PullRequestDTO{PullRequestID: req.PullRequestID, PullRequestName: req.PullRequestName, AuthorID: req.AuthorID, Status: dto.StatusOpen, AssignedReviewers: reviewers}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
