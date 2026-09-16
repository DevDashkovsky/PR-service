package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"pr-service/internal/domain/pr"
	"pr-service/internal/domain/teams"
	"pr-service/internal/domain/user"
	"pr-service/internal/http/handlers"
)

type readinessStub struct {
	err    error
	called bool
}

func (r *readinessStub) Ping(context.Context) error {
	r.called = true
	return r.err
}

func TestRouterErrorResponses(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		path     string
		wantCode int
		wantErr  string
	}{
		{
			name:     "not found",
			method:   http.MethodGet,
			path:     "/missing",
			wantCode: http.StatusNotFound,
			wantErr:  "NOT_FOUND",
		},
		{
			name:     "method not allowed",
			method:   http.MethodGet,
			path:     "/team/add",
			wantCode: http.StatusMethodNotAllowed,
			wantErr:  "METHOD_NOT_ALLOWED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newTestRouter(&readinessStub{})
			response := httptest.NewRecorder()
			request := httptest.NewRequest(tt.method, tt.path, http.NoBody)

			router.ServeHTTP(response, request)

			if response.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantCode)
			}
			if response.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("content type = %q, want application/json", response.Header().Get("Content-Type"))
			}
			assertErrorResponse(t, response, tt.wantErr)
		})
	}
}

func TestHealthEndpoints(t *testing.T) {
	t.Run("liveness does not check readiness", func(t *testing.T) {
		readiness := &readinessStub{err: errors.New("database is unavailable")}
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)

		newTestRouter(readiness).ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
		}
		if readiness.called {
			t.Fatal("liveness endpoint called readiness checker")
		}
	})

	t.Run("ready", func(t *testing.T) {
		readiness := &readinessStub{}
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody)

		newTestRouter(readiness).ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
		}
		if !readiness.called {
			t.Fatal("readiness checker was not called")
		}
	})

	t.Run("not ready", func(t *testing.T) {
		readiness := &readinessStub{err: errors.New("database is unavailable")}
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody)

		newTestRouter(readiness).ServeHTTP(response, request)

		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
		}
		assertErrorResponse(t, response, "SERVICE_UNAVAILABLE")
	})

	t.Run("nil checker is not ready", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody)

		newTestRouter(nil).ServeHTTP(response, request)

		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
		}
		assertErrorResponse(t, response, "SERVICE_UNAVAILABLE")
	})
}

func newTestRouter(readiness ReadinessChecker) http.Handler {
	teamHandler := handlers.NewTeamHandler(teams.NewService(nil))
	userHandler := handlers.NewUserHandler(user.NewService(nil))
	prHandler := handlers.NewPRHandler(pr.NewService(nil))
	return NewRouter(teamHandler, userHandler, prHandler, readiness)
}
