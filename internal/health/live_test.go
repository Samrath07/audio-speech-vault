package health

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLive(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodGet,
		"/health/live",
		nil,
	)
	response := httptest.NewRecorder()

	Live().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"status code = %d, want %d",
			response.Code,
			http.StatusOK,
		)
	}

	contentType := response.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf(
			"Content-Type = %q, want application/json",
			contentType,
		)
	}

	expectedBody := `{"status":"ok","service":"audio-speech-vault"}`
	if response.Body.String() != expectedBody {
		t.Errorf(
			"body = %q, want %q",
			response.Body.String(),
			expectedBody,
		)
	}
}
