package user

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"pr-service/internal/domain/dto"
	"pr-service/pkg/validator"
)

type userRepositoryStub struct {
	getUserReviews func(context.Context, string, dto.UserReviewsPageRequest) (dto.UserReviewsPage, error)
}

func (s *userRepositoryStub) GetUserReviews(
	ctx context.Context,
	userID string,
	page dto.UserReviewsPageRequest,
) (dto.UserReviewsPage, error) {
	return s.getUserReviews(ctx, userID, page)
}

func (s *userRepositoryStub) SetUserActive(context.Context, string, bool) (*dto.UserDTO, error) {
	return nil, nil
}

func (s *userRepositoryStub) GetUser(context.Context, string) (*dto.UserDTO, error) {
	return nil, nil
}

func (s *userRepositoryStub) GetTeamByName(context.Context, string) (*dto.TeamDTO, error) {
	return nil, nil
}

func TestService_GetUserReviews_DefaultLimit(t *testing.T) {
	t.Parallel()

	repo := &userRepositoryStub{
		getUserReviews: func(
			_ context.Context,
			userID string,
			page dto.UserReviewsPageRequest,
		) (dto.UserReviewsPage, error) {
			if userID != "reviewer" {
				t.Fatalf("userID = %q, want reviewer", userID)
			}
			if page.Limit != defaultReviewsLimit || page.Cursor != nil {
				t.Fatalf("page = %+v, want default limit without cursor", page)
			}
			return dto.UserReviewsPage{
				PullRequests: []dto.PullRequestShortDTO{{PullRequestID: "pr-1"}},
			}, nil
		},
	}

	response, err := NewService(repo).GetUserReviews(t.Context(), "reviewer", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if response.UserID != "reviewer" || len(response.PullRequests) != 1 {
		t.Fatalf("response = %+v", response)
	}
	if response.NextCursor != "" {
		t.Fatalf("next_cursor = %q, want empty", response.NextCursor)
	}
}

func TestService_GetUserReviews_CursorRoundTrip(t *testing.T) {
	t.Parallel()

	boundary := dto.UserReviewsCursor{
		CreatedAt:     time.Date(2026, time.September, 16, 10, 11, 12, 345678000, time.UTC),
		PullRequestID: "pr-Я",
	}
	calls := 0
	repo := &userRepositoryStub{
		getUserReviews: func(
			_ context.Context,
			_ string,
			page dto.UserReviewsPageRequest,
		) (dto.UserReviewsPage, error) {
			calls++
			if page.Limit != 1 {
				t.Fatalf("limit = %d, want 1", page.Limit)
			}
			if calls == 1 {
				if page.Cursor != nil {
					t.Fatalf("first cursor = %+v, want nil", page.Cursor)
				}
				return dto.UserReviewsPage{
					PullRequests: []dto.PullRequestShortDTO{{PullRequestID: "pr-Я"}},
					NextCursor:   &boundary,
				}, nil
			}
			if page.Cursor == nil || !page.Cursor.CreatedAt.Equal(boundary.CreatedAt) ||
				page.Cursor.PullRequestID != boundary.PullRequestID {
				t.Fatalf("decoded cursor = %+v, want %+v", page.Cursor, boundary)
			}
			return dto.UserReviewsPage{PullRequests: []dto.PullRequestShortDTO{}}, nil
		},
	}
	service := NewService(repo)

	first, err := service.GetUserReviews(t.Context(), "reviewer", "1", "")
	if err != nil {
		t.Fatal(err)
	}
	if first.NextCursor == "" {
		t.Fatal("next_cursor is empty")
	}
	second, err := service.GetUserReviews(t.Context(), "reviewer", "1", first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if second.NextCursor != "" || calls != 2 {
		t.Fatalf("second response = %+v, calls = %d", second, calls)
	}
}

func TestService_GetUserReviews_InvalidLimit(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"0", "101", "-1", "1.5", "invalid"} {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			service := NewService(&userRepositoryStub{
				getUserReviews: func(
					context.Context,
					string,
					dto.UserReviewsPageRequest,
				) (dto.UserReviewsPage, error) {
					t.Fatal("repository must not be called")
					return dto.UserReviewsPage{}, nil
				},
			})

			_, err := service.GetUserReviews(t.Context(), "reviewer", value, "")
			if !errors.Is(err, validator.ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestParseReviewsLimit_Boundaries(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		value string
		want  int
	}{
		{value: "", want: 50},
		{value: "1", want: 1},
		{value: "100", want: 100},
	} {
		limit, err := parseReviewsLimit(testCase.value)
		if err != nil {
			t.Fatalf("parseReviewsLimit(%q): %v", testCase.value, err)
		}
		if limit != testCase.want {
			t.Fatalf("parseReviewsLimit(%q) = %d, want %d", testCase.value, limit, testCase.want)
		}
	}
}

func TestService_GetUserReviews_InvalidCursor(t *testing.T) {
	t.Parallel()

	encode := func(value string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(value))
	}
	testCases := []string{
		"not-base64!",
		base64.URLEncoding.EncodeToString([]byte("x")),
		encode(`not-json`),
		encode(`{"created_at":"invalid","pull_request_id":"pr"}`),
		encode(`{"created_at":"2026-09-16T10:00:00Z","pull_request_id":""}`),
		encode(`{"created_at":"2026-09-16T10:00:00Z","pull_request_id":"pr","extra":true}`),
	}
	service := NewService(&userRepositoryStub{
		getUserReviews: func(
			context.Context,
			string,
			dto.UserReviewsPageRequest,
		) (dto.UserReviewsPage, error) {
			t.Fatal("repository must not be called")
			return dto.UserReviewsPage{}, nil
		},
	})

	for _, value := range testCases {
		_, err := service.GetUserReviews(t.Context(), "reviewer", "1", value)
		if !errors.Is(err, validator.ErrInvalidInput) {
			t.Fatalf("cursor %q: error = %v, want ErrInvalidInput", value, err)
		}
	}
}
