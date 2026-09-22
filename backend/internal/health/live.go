package health

import (
	"io"
	"net/http"
)

func Live() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		_, _ = io.WriteString(
			w,
			`{"status":"ok","service":"audio-speech-vault"}`,
		)
	})
}
