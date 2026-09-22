package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shivam3746/audio-speech-vault/internal/config"
	"github.com/shivam3746/audio-speech-vault/internal/health"
	"github.com/shivam3746/audio-speech-vault/internal/platform/database"
	"github.com/shivam3746/audio-speech-vault/internal/platform/httpserver"
	"github.com/shivam3746/audio-speech-vault/internal/platform/logging"
)

const version = "dev"

func main() {
	if err := run(); err != nil {
		log.Fatalf("application failed: %v", err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	logger, err := logging.New(
		cfg.AppEnv,
		cfg.LogLevel,
		version,
	)
	if err != nil {
		return fmt.Errorf("create logger: %w", err)
	}

	slog.SetDefault(logger)

	databaseContext, cancelDatabase := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	pool, err := database.Open(databaseContext, cfg.DatabaseURL)
	cancelDatabase()
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()
	logger.Info("database connected")

	mux := http.NewServeMux()
	mux.Handle("GET /health/live", health.Live())
	mux.Handle("GET /health/ready", health.Ready(pool))

	handler := httpserver.RequestID(
		httpserver.RequestLogger(logger)(
			httpserver.Recovery(logger)(mux),
		),
	)

	server := &http.Server{
		Addr:              cfg.HTTPAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownSignal, stopSignals := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stopSignals()

	err = httpserver.ListenAndServe(
		shutdownSignal,
		server,
		logger,
		cfg.ShutdownTimeout,
	)
	stopSignals()

	return err
}
