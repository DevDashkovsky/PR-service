package handlers

import (
	"AvitoTech/internal/domain/dto"
	"AvitoTech/internal/domain/teams"
	"AvitoTech/pkg/logger"
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error: dto.Error{
				Code:    teams.BadRequest,
				Message: "invalid request body",
			},
		})
		return
	}

	logger.Log.Info("Создание команды",
		zap.String("team_name", req.TeamName),
		zap.Int("members_count", len(req.Members)),
	)

	ctx := r.Context()
	err := h.service.CreateTeam(ctx, req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if errors.Is(err, teams.ErrTeamExists) {
			logger.Log.Warn("Команда уже существует", zap.String("team_name", req.TeamName))
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error: dto.Error{
					Code:    teams.TeamExistsCode,
					Message: "team_name already exists",
				},
			})
			return
		}

		logger.Log.Error("Ошибка при создании команды",
			zap.String("team_name", req.TeamName),
			zap.Error(err),
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error: dto.Error{
				Code:    teams.InternalError,
				Message: "internal server error",
			},
		})
		return
	}

	logger.Log.Info("Команда успешно создана", zap.String("team_name", req.TeamName))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(dto.TeamResponse{Team: req})
}

func (h *TeamHandler) GetTeam(w http.ResponseWriter, r *http.Request) {
	teamName := r.URL.Query().Get("team_name")
	if teamName == "" {
		logger.Log.Warn("Запрос получения команды без team_name")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error: dto.Error{
				Code:    teams.BadRequest,
				Message: "team_name is required",
			},
		})
		return
	}

	logger.Log.Info("Получение команды", zap.String("team_name", teamName))

	ctx := r.Context()
	t, err := h.service.GetTeam(ctx, teamName)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if errors.Is(err, teams.ErrTeamNotFound) {
			logger.Log.Warn("Команда не найдена", zap.String("team_name", teamName))
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error: dto.Error{
					Code:    teams.TeamNotFound,
					Message: "resource not found",
				},
			})
			return
		}
		logger.Log.Error("Ошибка при получении команды", zap.String("team_name", teamName), zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error: dto.Error{
				Code:    teams.InternalError,
				Message: "internal server error",
			},
		})
		return
	}

	logger.Log.Info("Команда успешно получена",
		zap.String("team_name", teamName),
		zap.Int("members_count", len(t.Members)),
	)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(t)
}
