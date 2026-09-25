package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/shivam3746/audio-speech-vault/internal/access"
	"github.com/shivam3746/audio-speech-vault/internal/analysis"
	"github.com/shivam3746/audio-speech-vault/internal/auth"
	"github.com/shivam3746/audio-speech-vault/internal/config"
	"github.com/shivam3746/audio-speech-vault/internal/health"
	"github.com/shivam3746/audio-speech-vault/internal/platform/database"
	"github.com/shivam3746/audio-speech-vault/internal/platform/httpserver"
	"github.com/shivam3746/audio-speech-vault/internal/platform/logging"
	platformmail "github.com/shivam3746/audio-speech-vault/internal/platform/mail"
	"github.com/shivam3746/audio-speech-vault/internal/workspace"
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
	shutdownSignal, stopSignals := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stopSignals()
	go analysis.NewWorker(pool, cfg.DataDir, logger).Run(shutdownSignal)

	mux := http.NewServeMux()
	mux.Handle("GET /health/live", health.Live())
	mux.Handle("GET /health/ready", health.Ready(pool))
	authHandler, err := auth.NewHandler(auth.NewStore(pool), logger, cfg.AppEnv != "development" && cfg.AppEnv != "test", cfg.FrontendOrigin)
	if err != nil {
		return fmt.Errorf("create authentication handler: %w", err)
	}
	authHandler.Register(mux)
	var mailSender platformmail.Sender
	if cfg.SMTPHost != "" {
		mailSender = platformmail.SMTPSender{Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUsername, Password: cfg.SMTPPassword, From: cfg.SMTPFrom}
	} else {
		mailSender = platformmail.FileSender{Directory: filepath.Join(cfg.DataDir, "mail-outbox")}
		logger.Warn("development mail is written to the local outbox", slog.String("directory", filepath.Join(cfg.DataDir, "mail-outbox")))
	}
	accessHandler := access.NewHandler(access.NewService(pool, mailSender, cfg.FrontendOrigin), authHandler, logger)
	accessHandler.Register(mux)
	workspace.NewHandler(workspace.NewStore(pool), authHandler, logger, cfg.DataDir).Register(mux)
	analysis.NewHandler(pool, authHandler, logger, cfg.DataDir).Register(mux)

	handler := httpserver.RequestID(
		httpserver.RequestLogger(logger)(
			httpserver.Recovery(logger)(mux),
		),
	)

	server := &http.Server{
		Addr:              cfg.HTTPAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}

	err = httpserver.ListenAndServe(
		shutdownSignal,
		server,
		logger,
		cfg.ShutdownTimeout,
	)
	stopSignals()

	return err
}
