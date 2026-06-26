package handlers

import (
	"encoding/json"
	"net/http"
	"pr-service/internal/domain/dto"
	"pr-service/pkg/logger"

	"go.uber.org/zap"
)

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		logger.Log.Error("Ошибка сериализации ответа", zap.Error(err))
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, dto.ErrorResponse{
		Error: dto.Error{
			Code:    code,
			Message: message,
		},
	})
}
