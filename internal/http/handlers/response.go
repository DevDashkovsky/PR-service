package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"pr-service/internal/domain/dto"
	"pr-service/pkg/logger"

	"go.uber.org/zap"
)

// WriteJSON writes a JSON response with the provided status code.
func WriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		logger.Log.Error("Ошибка сериализации ответа", zap.Error(err))
	}
}

// WriteError writes an API error response with the provided status code.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, dto.ErrorResponse{
		Error: dto.Error{
			Code:    code,
			Message: message,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	WriteJSON(w, status, payload)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	WriteError(w, status, code, message)
}

func writeContextError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "GATEWAY_TIMEOUT", "request deadline exceeded")
		return true
	case errors.Is(err, context.Canceled):
		return true
	default:
		return false
	}
}
