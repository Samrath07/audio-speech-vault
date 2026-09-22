package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeDatabase struct {
	err error
}

func (db fakeDatabase) Ping(context.Context) error {
	return db.err
}

func TestReadyWhenDatabaseIsAvailable(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

	Ready(fakeDatabase{}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if response.Body.String() != `{"status":"ok"}` {
		t.Fatalf("body = %q", response.Body.String())
	}
}

func TestReadyWhenDatabaseIsUnavailable(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

	Ready(fakeDatabase{err: errors.New("database unavailable")}).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	if response.Body.String() != `{"status":"unavailable"}` {
		t.Fatalf("body = %q", response.Body.String())
	}
}
