package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pr-service/internal/domain/dto"
	"pr-service/internal/domain/user"
)

type handlerUserRepository struct {
	getUserReviews func(context.Context, string, dto.UserReviewsPageRequest) (dto.UserReviewsPage, error)
}

func (r *handlerUserRepository) GetUserReviews(
	ctx context.Context,
	userID string,
	page dto.UserReviewsPageRequest,
) (dto.UserReviewsPage, error) {
	return r.getUserReviews(ctx, userID, page)
}

func (r *handlerUserRepository) SetUserActive(context.Context, string, bool) (*dto.UserDTO, error) {
	return nil, nil
}

func (r *handlerUserRepository) GetUser(context.Context, string) (*dto.UserDTO, error) {
	return nil, nil
}

func (r *handlerUserRepository) GetTeamByName(context.Context, string) (*dto.TeamDTO, error) {
	return nil, nil
}

func TestUserHandler_GetUserReviews_InvalidInput(t *testing.T) {
	t.Parallel()

	handler := NewUserHandler(user.NewService(nil))
	for _, target := range []string{
		"/users/getReview?user_id=reviewer&limit=0",
		"/users/getReview?user_id=reviewer&cursor=invalid!",
		"/users/getReview?user_id=%FF",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(http.MethodGet, target, http.NoBody)
			response := httptest.NewRecorder()

			handler.GetUserReviews(response, request)

			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), user.BadRequest) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestUserHandler_GetUserReviews_ReturnsNextCursor(t *testing.T) {
	t.Parallel()

	repo := &handlerUserRepository{
		getUserReviews: func(
			_ context.Context,
			userID string,
			page dto.UserReviewsPageRequest,
		) (dto.UserReviewsPage, error) {
			if userID != "reviewer" || page.Limit != 1 || page.Cursor != nil {
				t.Fatalf("userID = %q, page = %+v", userID, page)
			}
			return dto.UserReviewsPage{
				PullRequests: []dto.PullRequestShortDTO{{PullRequestID: "pr-2"}},
				NextCursor: &dto.UserReviewsCursor{
					CreatedAt:     time.Date(2026, time.September, 16, 10, 0, 0, 0, time.UTC),
					PullRequestID: "pr-2",
				},
			}, nil
		},
	}
	handler := NewUserHandler(user.NewService(repo))
	request := httptest.NewRequest(
		http.MethodGet,
		"/users/getReview?user_id=reviewer&limit=1",
		http.NoBody,
	)
	response := httptest.NewRecorder()

	handler.GetUserReviews(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload dto.GetUserReviewsResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.UserID != "reviewer" || len(payload.PullRequests) != 1 || payload.NextCursor == "" {
		t.Fatalf("payload = %+v", payload)
	}
}
