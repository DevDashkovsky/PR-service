package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"pr-service/internal/http/handlers"
	"pr-service/pkg/logger"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

const (
	_requestIDHeader    = "X-Request-Id"
	_maxRequestIDLength = 128
)

type requestIDContextKey struct{}

type responseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

var _fallbackRequestID atomic.Uint64

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get(_requestIDHeader))
		if requestID == "" || len(requestID) > _maxRequestIDLength {
			requestID = newRequestID()
		}

		w.Header().Set(_requestIDHeader, requestID)
		ctx := context.WithValue(r.Context(), requestIDContextKey{}, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		response := &responseWriter{ResponseWriter: w}

		next.ServeHTTP(response, r)

		status := response.status
		if status == 0 {
			status = http.StatusOK
		}
		fields := []zap.Field{
			zap.String("request_id", requestIDFromContext(r.Context())),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("route", routePattern(r)),
			zap.Int("status", status),
			zap.Int("bytes", response.bytes),
			zap.Duration("duration", time.Since(startedAt)),
		}
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			logger.Log.Debug("HTTP request", fields...)
			return
		}

		logger.Log.Info("HTTP request", fields...)
	})
}

func recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := &responseWriter{ResponseWriter: w}
		defer func() {
			panicValue := recover()
			if panicValue == nil {
				return
			}

			logger.Log.Error(
				"Panic while processing HTTP request",
				zap.String("request_id", requestIDFromContext(r.Context())),
				zap.Any("panic", panicValue),
				zap.Stack("stack"),
			)
			if response.status == 0 {
				writeInternalError(response)
			}
		}()

		next.ServeHTTP(response, r)
	})
}

func (w *responseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}

	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}

	written, err := w.ResponseWriter.Write(body)
	w.bytes += written
	return written, err
}

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func newRequestID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}

	sequence := _fallbackRequestID.Add(1)
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.FormatUint(sequence, 36)
}

func requestIDFromContext(ctx context.Context) string {
	requestID, ok := ctx.Value(requestIDContextKey{}).(string)
	if !ok {
		return ""
	}
	return requestID
}

func routePattern(r *http.Request) string {
	pattern := chi.RouteContext(r.Context()).RoutePattern()
	if pattern == "" {
		return "unmatched"
	}
	return pattern
}

func writeInternalError(w http.ResponseWriter) {
	handlers.WriteError(
		w,
		http.StatusInternalServerError,
		"INTERNAL_ERROR",
		"internal server error",
	)
}
