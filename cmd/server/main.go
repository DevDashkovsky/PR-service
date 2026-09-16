package main

import (
	"context"
	"errors"
	"log"
	stdhttp "net/http"
	"os"
	"os/signal"
	"pr-service/internal/domain/pr"
	"pr-service/internal/domain/teams"
	"pr-service/internal/domain/user"
	"pr-service/internal/http"
	"pr-service/internal/http/handlers"
	"pr-service/internal/infrastructure/postgres"
	"pr-service/pkg/logger"
	"syscall"
	"time"

	"go.uber.org/zap"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	isDev := os.Getenv("ENVIRONMENT") != "production"
	if err := logger.Init(isDev); err != nil {
		return err
	}
	defer logger.Sync()

	logger.Log.Info("Запуск сервиса",
		zap.Bool("development", isDev),
		zap.String("version", "1.0.0"),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := postgres.NewDBWithContext(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err := db.CloseDB(); err != nil {
			logger.Log.Error("Ошибка закрытия БД", zap.Error(err))
		}
	}()
	if err := db.RunMigrations(ctx); err != nil {
		return err
	}

	teamRepo := postgres.NewTeamRepo(db)
	userRepo := postgres.NewUserRepo(db)
	prRepo := postgres.NewPRRepo(db)

	teamService := teams.NewService(teamRepo)
	teamHandler := handlers.NewTeamHandler(teamService)

	userService := user.NewService(userRepo)
	userHandler := handlers.NewUserHandler(userService)

	prService := pr.NewService(prRepo)
	prHandler := handlers.NewPRHandler(prService)

	router := http.NewRouter(teamHandler, userHandler, prHandler, db)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := http.NewServer(":"+port, router)

	serverErr := make(chan error, 1)
	go func() {
		logger.Log.Info("Запуск HTTP сервера", zap.String("port", port))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		logger.Log.Info("Получен сигнал завершения, останавливаем сервер")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		if closeErr := server.Close(); closeErr != nil {
			logger.Log.Error("Ошибка закрытия HTTP сервера", zap.Error(closeErr))
		}
		return err
	} else {
		logger.Log.Info("Сервер корректно остановлен")
	}
	return nil
}
