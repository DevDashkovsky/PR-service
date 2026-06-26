package interfaces

import (
	"pr-service/internal/domain/dto"
	"context"
	"time"
)

type PRRepository interface {
	PRExists(ctx context.Context, prID string) (bool, error)

	CreatePRWithReviewers(ctx context.Context, prID, prName, authorID string, reviewerIDs []string) error

	GetPR(ctx context.Context, prID string) (*dto.PullRequestDTO, error)

	SetMerged(ctx context.Context, prID string, mergedAt time.Time) error

	GetReviewers(ctx context.Context, prID string) ([]string, error)
	ReplaceReviewer(ctx context.Context, prID, oldReviewerID, newReviewerID string) error
	IsReviewerAssigned(ctx context.Context, prID, reviewerID string) (bool, error)
}
