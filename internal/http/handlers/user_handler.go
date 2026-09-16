package handlers

import (
	"context"
	"errors"
	"net/http"
	"pr-service/internal/domain/dto"
	"pr-service/internal/domain/user"
	"pr-service/pkg/logger"
	"pr-service/pkg/validator"

	"go.uber.org/zap"
)

type userService interface {
	GetUserReviews(context.Context, string, string, string) (*dto.GetUserReviewsResponse, error)
	SetUserActive(context.Context, string, bool) (*dto.UserDTO, error)
}

type UserHandler struct {
	service userService
}

func NewUserHandler(service userService) *UserHandler {
	return &UserHandler{service: service}
}

func (h *UserHandler) GetUserReviews(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")

	if userID == "" {
		logger.Log.Warn("Запрос получения PR без user_id")
		writeError(w, http.StatusBadRequest, user.BadRequest, "user_id is required")
		return
	}

	logger.Log.Info("Получение PR пользователя", zap.String("user_id", userID))

	response, err := h.service.GetUserReviews(
		r.Context(),
		userID,
		r.URL.Query().Get("limit"),
		r.URL.Query().Get("cursor"),
	)
	if err != nil {
		if errors.Is(err, validator.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, user.BadRequest, err.Error())
			return
		}
		if writeContextError(w, err) {
			return
		}
		logger.Log.Error("Ошибка получения PR пользователя",
			zap.String("user_id", userID),
			zap.Error(err),
		)
		writeError(w, http.StatusInternalServerError, user.InternalError, "internal server error")
		return
	}

	logger.Log.Info("PR пользователя успешно получены",
		zap.String("user_id", userID),
		zap.Int("pr_count", len(response.PullRequests)),
	)

	writeJSON(w, http.StatusOK, response)
}

func (h *UserHandler) SetUserActive(w http.ResponseWriter, r *http.Request) {
	var req dto.SetUserActiveRequest
	if err := decodeRequest(w, r, &req); err != nil {
		logger.Log.Warn("Неверный формат запроса изменения активности", zap.Error(err))
		writeError(w, http.StatusBadRequest, user.BadRequest, "invalid request body")
		return
	}

	logger.Log.Info("Изменение активности пользователя",
		zap.String("user_id", req.UserID),
		zap.Bool("is_active", req.IsActive),
	)

	u, err := h.service.SetUserActive(r.Context(), req.UserID, req.IsActive)
	if err != nil {
		switch {
		case errors.Is(err, validator.ErrInvalidInput):
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		case errors.Is(err, user.ErrUserNotFound):
			logger.Log.Warn("Пользователь не найден", zap.String("user_id", req.UserID))
			writeError(w, http.StatusNotFound, user.UserNotFound, "user not found")
		default:
			if writeContextError(w, err) {
				return
			}
			logger.Log.Error("Ошибка изменения активности пользователя",
				zap.String("user_id", req.UserID),
				zap.Error(err),
			)
			writeError(w, http.StatusInternalServerError, user.InternalError, "internal server error")
		}
		return
	}

	logger.Log.Info("Активность пользователя успешно изменена",
		zap.String("user_id", req.UserID),
		zap.Bool("is_active", req.IsActive),
	)

	writeJSON(w, http.StatusOK, dto.UserResponse{User: *u})
}
