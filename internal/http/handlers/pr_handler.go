package handlers

import (
	"errors"
	"net/http"
	"pr-service/internal/domain/dto"
	"pr-service/internal/domain/pr"
	"pr-service/pkg/logger"
	"pr-service/pkg/validator"

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
	if err := decodeRequest(w, r, &req); err != nil {
		logger.Log.Warn("Неверный формат запроса создания PR", zap.Error(err))
		writeError(w, http.StatusBadRequest, pr.BadRequest, "invalid request body")
		return
	}

	logger.Log.Info("Запрос на создание PR",
		zap.String("pr_id", req.PullRequestID),
		zap.String("author_id", req.AuthorID),
	)

	pullRequest, err := h.service.CreatePR(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, validator.ErrInvalidInput):
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		case errors.Is(err, pr.ErrPRExists):
			logger.Log.Warn("PR уже существует", zap.String("pr_id", req.PullRequestID))
			writeError(w, http.StatusConflict, pr.PRExists, "PR id already exists")
		case errors.Is(err, pr.ErrAuthorNotFound), errors.Is(err, pr.ErrAuthorNoTeam):
			logger.Log.Warn("Автор не найден или без команды", zap.String("author_id", req.AuthorID))
			writeError(w, http.StatusNotFound, pr.NotFound, "author not found")
		default:
			logger.Log.Error("Ошибка при создании PR", zap.Error(err))
			writeError(w, http.StatusInternalServerError, pr.InternalError, "internal server error")
		}
		return
	}

	logger.Log.Info("PR успешно создан",
		zap.String("pr_id", pullRequest.PullRequestID),
		zap.Int("reviewers_count", len(pullRequest.AssignedReviewers)),
	)

	writeJSON(w, http.StatusCreated, dto.PullRequestResponse{PR: *pullRequest})
}

func (h *PRHandler) MergePR(w http.ResponseWriter, r *http.Request) {
	var req dto.MergePullRequestRequest
	if err := decodeRequest(w, r, &req); err != nil {
		logger.Log.Warn("Неверный формат запроса мерджа PR", zap.Error(err))
		writeError(w, http.StatusBadRequest, pr.BadRequest, "invalid request body")
		return
	}

	logger.Log.Info("Запрос на мердж PR", zap.String("pr_id", req.PullRequestID))

	mergedPR, err := h.service.MergePR(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, validator.ErrInvalidInput):
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		case errors.Is(err, pr.ErrPRNotFound):
			logger.Log.Warn("PR не найден", zap.String("pr_id", req.PullRequestID))
			writeError(w, http.StatusNotFound, pr.NotFound, "pull request not found")
		default:
			logger.Log.Error("Ошибка при мердже PR", zap.Error(err))
			writeError(w, http.StatusInternalServerError, pr.InternalError, "internal server error")
		}
		return
	}

	logger.Log.Info("PR успешно смерджен",
		zap.String("pr_id", mergedPR.PullRequestID),
		zap.String("merged_at", mergedPR.MergedAt),
	)

	writeJSON(w, http.StatusOK, dto.MergePullRequestResponse{PR: *mergedPR})
}

func (h *PRHandler) ReassignPR(w http.ResponseWriter, r *http.Request) {
	var req dto.ReassignPullRequestRequest
	if err := decodeRequest(w, r, &req); err != nil {
		logger.Log.Warn("Неверный формат запроса переназначения", zap.Error(err))
		writeError(w, http.StatusBadRequest, pr.BadRequest, "invalid request body")
		return
	}

	logger.Log.Info("Запрос на переназначение ревьювера",
		zap.String("pr_id", req.PullRequestID),
		zap.String("old_user_id", req.OldUserID),
	)

	response, err := h.service.ReassignReviewer(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, validator.ErrInvalidInput):
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		case errors.Is(err, pr.ErrPRNotFound):
			logger.Log.Warn("PR не найден", zap.String("pr_id", req.PullRequestID))
			writeError(w, http.StatusNotFound, pr.NotFound, "pull request not found")
		case errors.Is(err, pr.ErrPRMerged):
			logger.Log.Warn("Попытка переназначить на смердженный PR", zap.String("pr_id", req.PullRequestID))
			writeError(w, http.StatusConflict, pr.PRMerged, "cannot reassign on merged PR")
		case errors.Is(err, pr.ErrNotAssigned):
			logger.Log.Warn("Ревьювер не назначен на PR",
				zap.String("pr_id", req.PullRequestID),
				zap.String("user_id", req.OldUserID),
			)
			writeError(w, http.StatusConflict, pr.NotAssigned, "reviewer is not assigned to this PR")
		case errors.Is(err, pr.ErrNoCandidate):
			logger.Log.Warn("Нет кандидатов для замены", zap.String("pr_id", req.PullRequestID))
			writeError(w, http.StatusConflict, pr.NoCandidate, "no active replacement candidate in team")
		default:
			logger.Log.Error("Ошибка при переназначении ревьювера", zap.Error(err))
			writeError(w, http.StatusInternalServerError, pr.InternalError, "internal server error")
		}
		return
	}

	logger.Log.Info("Ревьювер успешно переназначен",
		zap.String("pr_id", req.PullRequestID),
		zap.String("replaced_by", response.ReplacedBy),
	)

	writeJSON(w, http.StatusOK, response)
}
