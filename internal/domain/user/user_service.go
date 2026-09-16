package user

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"pr-service/internal/domain/dto"
	"pr-service/internal/domain/interfaces"
	"pr-service/pkg/validator"
)

const (
	BadRequest    = "BAD_REQUEST"
	InternalError = "INTERNAL_ERROR"
	UserNotFound  = "NOT_FOUND"

	defaultReviewsLimit = 50
	maxReviewsLimit     = 100
	maxCursorLength     = 2048
)

var (
	ErrUserNotFound = errors.New("пользователь не найден")
)

type Service struct {
	repo interfaces.UserRepository
}

type reviewCursorPayload struct {
	CreatedAt     string `json:"created_at"`
	PullRequestID string `json:"pull_request_id"`
}

func NewService(repo interfaces.UserRepository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetUserReviews(
	ctx context.Context,
	userID string,
	limitValue string,
	cursorValue string,
) (*dto.GetUserReviewsResponse, error) {
	if err := validator.ValidateUserID(userID); err != nil {
		return nil, err
	}

	limit, err := parseReviewsLimit(limitValue)
	if err != nil {
		return nil, err
	}
	cursor, err := decodeReviewsCursor(cursorValue)
	if err != nil {
		return nil, err
	}

	page, err := s.repo.GetUserReviews(ctx, userID, dto.UserReviewsPageRequest{
		Limit:  limit,
		Cursor: cursor,
	})
	if err != nil {
		return nil, fmt.Errorf("get user reviews: %w", err)
	}

	response := &dto.GetUserReviewsResponse{
		UserID:       userID,
		PullRequests: page.PullRequests,
	}
	if page.NextCursor != nil {
		response.NextCursor, err = encodeReviewsCursor(*page.NextCursor)
		if err != nil {
			return nil, fmt.Errorf("encode reviews cursor: %w", err)
		}
	}

	return response, nil
}

func (s *Service) SetUserActive(ctx context.Context, userID string, isActive bool) (*dto.UserDTO, error) {
	if err := validator.ValidateUserID(userID); err != nil {
		return nil, fmt.Errorf("invalid user_id: %w", err)
	}

	user, err := s.repo.SetUserActive(ctx, userID, isActive)
	if err != nil {
		return nil, err
	}

	return user, nil
}

func parseReviewsLimit(value string) (int, error) {
	if value == "" {
		return defaultReviewsLimit, nil
	}

	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > maxReviewsLimit {
		return 0, fmt.Errorf("%w: limit must be an integer between 1 and 100", validator.ErrInvalidInput)
	}

	return limit, nil
}

func encodeReviewsCursor(cursor dto.UserReviewsCursor) (string, error) {
	payload, err := json.Marshal(reviewCursorPayload{
		CreatedAt:     cursor.CreatedAt.UTC().Format(time.RFC3339Nano),
		PullRequestID: cursor.PullRequestID,
	})
	if err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeReviewsCursor(value string) (*dto.UserReviewsCursor, error) {
	if value == "" {
		return nil, nil
	}
	if len(value) > maxCursorLength {
		return nil, invalidCursorError()
	}

	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != value {
		return nil, invalidCursorError()
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload reviewCursorPayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, invalidCursorError()
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, invalidCursorError()
	}

	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil || validator.ValidatePullRequestID(payload.PullRequestID) != nil {
		return nil, invalidCursorError()
	}

	return &dto.UserReviewsCursor{
		CreatedAt:     createdAt.UTC(),
		PullRequestID: payload.PullRequestID,
	}, nil
}

func invalidCursorError() error {
	return fmt.Errorf("%w: cursor is invalid", validator.ErrInvalidInput)
}
