package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeStartsAndShutsDownGracefully(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: time.Second,
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- Serve(ctx, server, listener, logger, time.Second)
	}()

	url := "http://" + listener.Addr().String() + "/health/live"
	requireStatusOK(t, url)

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve() returned error: %v", err)
		}

	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down before timeout")
	}
}

func requireStatusOK(t *testing.T, url string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	var lastErr error

	for time.Now().Before(deadline) {
		response, err := http.Get(url)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}

		lastErr = err
		time.Sleep(25 * time.Millisecond)
	}

	t.Fatalf("GET %s did not return 200 before timeout: %v", url, lastErr)
}
