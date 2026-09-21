package httpserver

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecoveryConvertsPanicToServerError(t *testing.T) {
	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	panicHandler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			panic("test panic")
		},
	)

	handler := RequestID(
		Recovery(logger)(panicHandler),
	)

	request := httptest.NewRequest(http.MethodGet, "/panic", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf(
			"status code = %d, want %d",
			response.Code,
			http.StatusInternalServerError,
		)
	}

	if response.Header().Get("X-Request-ID") == "" {
		t.Error("expected X-Request-ID response header")
	}
}
