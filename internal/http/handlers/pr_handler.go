package handlers

import (
	"AvitoTech/internal/domain/dto"
	"AvitoTech/internal/domain/pr"
	"AvitoTech/pkg/logger"
	"encoding/json"
	"errors"
	"net/http"

	"go.uber.org/zap"
)

type PRHandler struct {
	service *pr.Service
}

func NewPRHandler(service *pr.Service) *PRHandler {
	return &PRHandler{service: service}
}

func (h *PRHandler) CreatePR(w http.ResponseWriter, r *http.Request) {
	var req dto.CreatePullRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Log.Warn("Неверный формат запроса создания PR", zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error: dto.Error{
				Code:    pr.BadRequest,
				Message: "invalid request body",
			},
		})
		return
	}

	logger.Log.Info("Запрос на создание PR",
		zap.String("pr_id", req.PullRequestID),
		zap.String("author_id", req.AuthorID),
	)

	pullRequest, err := h.service.CreatePR(r.Context(), req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if errors.Is(err, pr.ErrPRExists) {
			logger.Log.Warn("PR уже существует", zap.String("pr_id", req.PullRequestID))
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error: dto.Error{
					Code:    pr.PRExists,
					Message: "PR id already exists",
				},
			})
			return
		}

		if errors.Is(err, pr.ErrAuthorNotFound) || errors.Is(err, pr.ErrAuthorNoTeam) {
			logger.Log.Warn("Автор не найден или без команды", zap.String("author_id", req.AuthorID))
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error: dto.Error{
					Code:    pr.NotFound,
					Message: "author not found",
				},
			})
			return
		}

		logger.Log.Error("Ошибка при создании PR", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error: dto.Error{
				Code:    pr.InternalError,
				Message: "internal server error",
			},
		})
		return
	}

	logger.Log.Info("PR успешно создан",
		zap.String("pr_id", pullRequest.PullRequestID),
		zap.Int("reviewers_count", len(pullRequest.AssignedReviewers)),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(dto.PullRequestResponse{PR: *pullRequest})
}

func (h *PRHandler) MergePR(w http.ResponseWriter, r *http.Request) {
	var req dto.MergePullRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Log.Warn("Неверный формат запроса мерджа PR", zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error: dto.Error{
				Code:    pr.BadRequest,
				Message: "invalid request body",
			},
		})
		return
	}

	logger.Log.Info("Запрос на мердж PR", zap.String("pr_id", req.PullRequestID))

	mergedPR, err := h.service.MergePR(r.Context(), req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")

		if errors.Is(err, pr.ErrPRNotFound) {
			logger.Log.Warn("PR не найден", zap.String("pr_id", req.PullRequestID))
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error: dto.Error{
					Code:    pr.NotFound,
					Message: "pull request not found",
				},
			})
			return
		}

		logger.Log.Error("Ошибка при мердже PR", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error: dto.Error{
				Code:    pr.InternalError,
				Message: "internal server error",
			},
		})
		return
	}

	logger.Log.Info("PR успешно смерджен",
		zap.String("pr_id", mergedPR.PullRequestID),
		zap.String("merged_at", mergedPR.MergedAt),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(dto.MergePullRequestResponse{PR: *mergedPR})
}

func (h *PRHandler) ReassignPR(w http.ResponseWriter, r *http.Request) {
	var req dto.ReassignPullRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Log.Warn("Неверный формат запроса переназначения", zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error: dto.Error{
				Code:    pr.BadRequest,
				Message: "invalid request body",
			},
		})
		return
	}

	logger.Log.Info("Запрос на переназначение ревьювера",
		zap.String("pr_id", req.PullRequestID),
		zap.String("old_user_id", req.OldUserID),
	)

	response, err := h.service.ReassignReviewer(r.Context(), req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")

		if errors.Is(err, pr.ErrPRNotFound) {
			logger.Log.Warn("PR не найден", zap.String("pr_id", req.PullRequestID))
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error: dto.Error{
					Code:    pr.NotFound,
					Message: "pull request not found",
				},
			})
			return
		}

		if errors.Is(err, pr.ErrPRMerged) {
			logger.Log.Warn("Попытка переназначить на смердженный PR",
				zap.String("pr_id", req.PullRequestID),
			)
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error: dto.Error{
					Code:    pr.PRMerged,
					Message: "cannot reassign on merged PR",
				},
			})
			return
		}

		if errors.Is(err, pr.ErrNotAssigned) {
			logger.Log.Warn("Ревьювер не назначен на PR",
				zap.String("pr_id", req.PullRequestID),
				zap.String("user_id", req.OldUserID),
			)
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error: dto.Error{
					Code:    pr.NotAssigned,
					Message: "reviewer is not assigned to this PR",
				},
			})
			return
		}

		if errors.Is(err, pr.ErrNoCandidate) {
			logger.Log.Warn("Нет кандидатов для замены", zap.String("pr_id", req.PullRequestID))
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error: dto.Error{
					Code:    pr.NoCandidate,
					Message: "no active replacement candidate in team",
				},
			})
			return
		}

		logger.Log.Error("Ошибка при переназначении ревьювера", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error: dto.Error{
				Code:    pr.InternalError,
				Message: "internal server error",
			},
		})
		return
	}

	logger.Log.Info("Ревьювер успешно переназначен",
		zap.String("pr_id", req.PullRequestID),
		zap.String("replaced_by", response.ReplacedBy),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}
