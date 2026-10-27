package interfaces

import (
	"context"
	"pr-service/internal/domain/dto"
	"time"
)

type PRRepository interface {
	WithinTransaction(context.Context, func(PRTransaction) error) error
}

type PRTransaction interface {
	BeginReassignment(context.Context, string, string, string) (*dto.ReassignPullRequestResponse, error)
	SaveReassignment(context.Context, string, *dto.ReassignPullRequestResponse) error
	PRExists(context.Context, string) (bool, error)
	GetPRForUpdate(context.Context, string) (*dto.PullRequestDTO, error)
	GetUser(context.Context, string) (*dto.UserDTO, error)
	GetTeamForAssignment(context.Context, string) (*dto.TeamDTO, error)
	CreatePRWithReviewers(context.Context, string, string, string, []string) error
	SetMerged(context.Context, string, time.Time) error
	ReplaceReviewer(context.Context, string, string, string) error
}
