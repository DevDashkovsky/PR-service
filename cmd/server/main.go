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
	isDev := os.Getenv("ENVIRONMENT") != "production"
	if err := logger.Init(isDev); err != nil {
		log.Fatal("Не удалось инициализировать логгер: ", err)
	}
	defer logger.Sync()

	logger.Log.Info("Запуск сервиса",
		zap.Bool("development", isDev),
		zap.String("version", "1.0.0"),
	)

	db, err := postgres.NewDB()
	if err != nil {
		logger.Log.Fatal("Ошибка подключения к базе данных", zap.Error(err))
	}
	defer db.CloseDB()

	teamRepo := postgres.NewTeamRepo(db)
	userRepo := postgres.NewUserRepo(db)
	prRepo := postgres.NewPRRepo(db)

	teamService := teams.NewService(teamRepo, userRepo)
	teamHandler := handlers.NewTeamHandler(teamService)

	userService := user.NewService(userRepo)
	userHandler := handlers.NewUserHandler(userService)

	prService := pr.NewService(prRepo, userRepo)
	prHandler := handlers.NewPRHandler(prService)

	router := http.NewRouter(teamHandler, userHandler, prHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := http.NewServer(":"+port, router)

	// Запускаем сервер в отдельной горутине, чтобы main мог ждать сигнал завершения.
	serverErr := make(chan error, 1)
	go func() {
		logger.Log.Info("Запуск HTTP сервера", zap.String("port", port))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			serverErr <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		logger.Log.Fatal("Ошибка запуска сервера", zap.Error(err))
	case sig := <-stop:
		logger.Log.Info("Получен сигнал завершения, останавливаем сервер", zap.String("signal", sig.String()))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Log.Error("Ошибка при остановке сервера", zap.Error(err))
	} else {
		logger.Log.Info("Сервер корректно остановлен")
	}
}
