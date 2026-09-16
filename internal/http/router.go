package http

import (
	"context"
	"net/http"
	"time"

	"pr-service/internal/http/handlers"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const maxRequestBody = 1 << 20

type ReadinessChecker interface {
	Ping(ctx context.Context) error
}

func NewRouter(
	teamHandler *handlers.TeamHandler,
	userHandler *handlers.UserHandler,
	prHandler *handlers.PRHandler,
	readiness ReadinessChecker,
) *chi.Mux {
	r := chi.NewRouter()

	r.Use(requestID)
	r.Use(accessLog)
	r.Use(recoverPanic)
	r.Use(middleware.RequestSize(maxRequestBody))
	r.Use(middleware.Timeout(25 * time.Second))

	r.Get("/healthz", healthz)
	r.Get("/readyz", readyz(readiness))

	r.Route("/team", func(r chi.Router) {
		r.Post("/add", teamHandler.CreateTeam)
		r.Get("/get", teamHandler.GetTeam)
	})

	r.Route("/users", func(r chi.Router) {
		r.Post("/setIsActive", userHandler.SetUserActive)
		r.Get("/getReview", userHandler.GetUserReviews)
	})

	r.Route("/pullRequest", func(r chi.Router) {
		r.Post("/create", prHandler.CreatePR)
		r.Post("/merge", prHandler.MergePR)
		r.Post("/reassign", prHandler.ReassignPR)
	})

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		handlers.WriteError(w, http.StatusNotFound, "NOT_FOUND", "resource not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		handlers.WriteError(
			w,
			http.StatusMethodNotAllowed,
			"METHOD_NOT_ALLOWED",
			"method not allowed",
		)
	})

	return r
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	handlers.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func readyz(readiness ReadinessChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if readiness == nil || readiness.Ping(ctx) != nil {
			handlers.WriteError(
				w,
				http.StatusServiceUnavailable,
				"SERVICE_UNAVAILABLE",
				"service unavailable",
			)
			return
		}

		handlers.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func NewServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
