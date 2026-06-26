package handlers

import (
	"pr-service/internal/domain/dto"
	"pr-service/internal/domain/teams"
	"pr-service/pkg/logger"
	"encoding/json"
	"errors"
	"net/http"

	"go.uber.org/zap"
)

type TeamHandler struct {
	service *teams.Service
}

func NewTeamHandler(service *teams.Service) *TeamHandler {
	return &TeamHandler{service: service}
}

func (h *TeamHandler) CreateTeam(w http.ResponseWriter, r *http.Request) {
	var req dto.TeamDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Log.Warn("Неверный формат запроса создания команды", zap.Error(err))
		writeError(w, http.StatusBadRequest, teams.BadRequest, "invalid request body")
		return
	}

	logger.Log.Info("Создание команды",
		zap.String("team_name", req.TeamName),
		zap.Int("members_count", len(req.Members)),
	)

	if err := h.service.CreateTeam(r.Context(), req); err != nil {
		switch {
		case errors.Is(err, teams.ErrTeamExists):
			logger.Log.Warn("Команда уже существует", zap.String("team_name", req.TeamName))
			writeError(w, http.StatusBadRequest, teams.TeamExistsCode, "team_name already exists")
		default:
			logger.Log.Error("Ошибка при создании команды",
				zap.String("team_name", req.TeamName),
				zap.Error(err),
			)
			writeError(w, http.StatusInternalServerError, teams.InternalError, "internal server error")
		}
		return
	}

	logger.Log.Info("Команда успешно создана", zap.String("team_name", req.TeamName))

	writeJSON(w, http.StatusCreated, dto.TeamResponse{Team: req})
}

func (h *TeamHandler) GetTeam(w http.ResponseWriter, r *http.Request) {
	teamName := r.URL.Query().Get("team_name")
	if teamName == "" {
		logger.Log.Warn("Запрос получения команды без team_name")
		writeError(w, http.StatusBadRequest, teams.BadRequest, "team_name is required")
		return
	}

	logger.Log.Info("Получение команды", zap.String("team_name", teamName))

	t, err := h.service.GetTeam(r.Context(), teamName)
	if err != nil {
		switch {
		case errors.Is(err, teams.ErrTeamNotFound):
			logger.Log.Warn("Команда не найдена", zap.String("team_name", teamName))
			writeError(w, http.StatusNotFound, teams.TeamNotFound, "resource not found")
		default:
			logger.Log.Error("Ошибка при получении команды", zap.String("team_name", teamName), zap.Error(err))
			writeError(w, http.StatusInternalServerError, teams.InternalError, "internal server error")
		}
		return
	}

	logger.Log.Info("Команда успешно получена",
		zap.String("team_name", teamName),
		zap.Int("members_count", len(t.Members)),
	)

	writeJSON(w, http.StatusOK, t)
}
