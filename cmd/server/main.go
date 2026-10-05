package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/aadityya4real/Url-shortener-service/internal/config"
	"github.com/aadityya4real/Url-shortener-service/internal/database"
	"github.com/aadityya4real/Url-shortener-service/internal/handler"
	"github.com/aadityya4real/Url-shortener-service/internal/middleware"
	"github.com/aadityya4real/Url-shortener-service/internal/repository"
	"github.com/aadityya4real/Url-shortener-service/internal/service"
	"github.com/aadityya4real/Url-shortener-service/internal/shortener"
	"github.com/aadityya4real/Url-shortener-service/migrations"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}

	if err := createDatabaseDirectory(cfg.DatabasePath); err != nil {
		logger.Error("create database directory", "error", err)
		os.Exit(1)
	}

	db, err := database.Open(cfg.DatabasePath)
	if err != nil {
		logger.Error("connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	migrationContext, cancelMigration := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelMigration()
	if err := database.Migrate(migrationContext, db, migrations.Files); err != nil {
		logger.Error("run database migrations", "error", err)
		os.Exit(1)
	}

	linkRepo := repository.NewSQLiteLink(db)
	userRepo := repository.NewSQLiteUser(db)
	sessionRepo := repository.NewSQLiteSession(db)

	userService := service.NewUserService(userRepo, sessionRepo, 24*time.Hour)
	linkService := service.NewLinkService(linkRepo, shortener.Generator{}, cfg.CodeLength)
	httpHandler := handler.New(linkService, userService, db, cfg.BaseURL, cfg.MaxBodyBytes)
	rootHandler := middleware.Chain(
		httpHandler.Routes(),
		middleware.RequestID,
		middleware.Recover(logger),
		middleware.Logging(logger),
		middleware.SecurityHeaders,
		middleware.Timeout(cfg.RequestTimeout),
	)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           rootHandler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("server started", "address", cfg.HTTPAddr, "base_url", cfg.BaseURL)
		serverErrors <- server.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	select {
	case signal := <-signals:
		logger.Info("shutdown signal received", "signal", signal.String())
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped unexpectedly", "error", err)
		}
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownContext); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		_ = server.Close()
	}
	logger.Info("server stopped")
}

func createDatabaseDirectory(path string) error {
	if path == ":memory:" {
		return nil
	}
	directory := filepath.Dir(path)
	if directory == "." {
		return nil
	}
	return os.MkdirAll(directory, 0o755)
}
