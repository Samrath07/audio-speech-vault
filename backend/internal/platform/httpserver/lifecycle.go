package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

func ListenAndServe(
	ctx context.Context,
	server *http.Server,
	logger *slog.Logger,
	shutdownTimeout time.Duration,
) error {
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", server.Addr, err)
	}

	return Serve(ctx, server, listener, logger, shutdownTimeout)
}

func Serve(
	ctx context.Context,
	server *http.Server,
	listener net.Listener,
	logger *slog.Logger,
	shutdownTimeout time.Duration,
) error {
	if shutdownTimeout <= 0 {
		return fmt.Errorf("shutdown timeout must be greater than zero")
	}

	serverErrors := make(chan error, 1)

	go func() {
		logger.Info(
			"server starting",
			slog.String("address", listener.Addr().String()),
		)

		serverErrors <- server.Serve(listener)
	}()

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}

		return nil

	case <-ctx.Done():
		logger.Info(
			"shutdown requested",
			slog.Duration("timeout", shutdownTimeout),
		)

		shutdownContext, cancel := context.WithTimeout(
			context.Background(),
			shutdownTimeout,
		)
		defer cancel()

		if err := server.Shutdown(shutdownContext); err != nil {
			_ = server.Close()
			return fmt.Errorf("graceful shutdown: %w", err)
		}

		if err := <-serverErrors; err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server stopped unexpectedly: %w", err)
		}

		logger.Info("server stopped")
		return nil
	}
}
