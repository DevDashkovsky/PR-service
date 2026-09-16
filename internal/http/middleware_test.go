package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pr-service/internal/domain/dto"
	"pr-service/pkg/logger"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const _testRequestID = "request-123"

func TestRequestID(t *testing.T) {
	tests := []struct {
		name          string
		giveRequestID string
		wantRequestID string
	}{
		{name: "propagates incoming ID", giveRequestID: _testRequestID, wantRequestID: _testRequestID},
		{name: "generates missing ID"},
		{name: "replaces excessively long ID", giveRequestID: strings.Repeat("x", 129)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var contextRequestID string
			handler := requestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				contextRequestID = requestIDFromContext(r.Context())
				w.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
			request.Header.Set(_requestIDHeader, tt.giveRequestID)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			responseRequestID := response.Header().Get(_requestIDHeader)
			if responseRequestID == "" {
				t.Fatal("response request ID is empty")
			}
			if contextRequestID != responseRequestID {
				t.Fatalf("context request ID %q differs from response ID %q", contextRequestID, responseRequestID)
			}
			if tt.wantRequestID != "" && responseRequestID != tt.wantRequestID {
				t.Fatalf("request ID = %q, want %q", responseRequestID, tt.wantRequestID)
			}
			if tt.wantRequestID == "" && responseRequestID == tt.giveRequestID {
				t.Fatalf("request ID %q was not generated", responseRequestID)
			}
		})
	}
}

func TestAccessLog(t *testing.T) {
	core, observed := observer.New(zap.InfoLevel)
	originalLogger := logger.Log
	logger.Log = zap.New(core)
	t.Cleanup(func() { logger.Log = originalLogger })

	router := chi.NewRouter()
	router.Use(requestID)
	router.Use(accessLog)
	router.Get("/items/{itemID}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("response"))
	})

	request := httptest.NewRequest(http.MethodGet, "/items/42", http.NoBody)
	request.Header.Set(_requestIDHeader, _testRequestID)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	entries := observed.All()
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	assertLogField(t, fields, "request_id", _testRequestID)
	assertLogField(t, fields, "method", http.MethodGet)
	assertLogField(t, fields, "path", "/items/42")
	assertLogField(t, fields, "route", "/items/{itemID}")
	assertLogField(t, fields, "status", int64(http.StatusCreated))
	assertLogField(t, fields, "bytes", int64(len("response")))
	if _, ok := fields["duration"]; !ok {
		t.Fatal("duration field is missing")
	}
}

func TestRecoverPanic(t *testing.T) {
	router := chi.NewRouter()
	router.Use(requestID)
	router.Use(recoverPanic)
	router.Get("/panic", func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})

	request := httptest.NewRequest(http.MethodGet, "/panic", http.NoBody)
	request.Header.Set(_requestIDHeader, _testRequestID)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q, want application/json", response.Header().Get("Content-Type"))
	}
	if response.Header().Get(_requestIDHeader) != _testRequestID {
		t.Fatalf("request ID = %q, want %s", response.Header().Get(_requestIDHeader), _testRequestID)
	}
	assertErrorResponse(t, response, "INTERNAL_ERROR")
}

func assertLogField(t *testing.T, fields map[string]any, name string, want any) {
	t.Helper()
	if fields[name] != want {
		t.Fatalf("%s = %#v, want %#v", name, fields[name], want)
	}
}

func assertErrorResponse(t *testing.T, response *httptest.ResponseRecorder, wantCode string) {
	t.Helper()
	var payload dto.ErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error.Code != wantCode {
		t.Fatalf("error code = %q, want %q", payload.Error.Code, wantCode)
	}
}
