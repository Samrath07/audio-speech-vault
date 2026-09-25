package access

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/shivam3746/audio-speech-vault/internal/auth"
)

type Handler struct {
	service *Service
	auth    *auth.Handler
	logger  *slog.Logger
}

func NewHandler(service *Service, authHandler *auth.Handler, logger *slog.Logger) *Handler {
	return &Handler{service: service, auth: authHandler, logger: logger}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/access-requests", h.create)
	mux.HandleFunc("POST /api/access-requests/verify", h.verify)
	mux.HandleFunc("GET /api/access-requests", h.list)
	mux.HandleFunc("POST /api/access-requests/{id}/approve", h.approve)
	mux.HandleFunc("POST /api/access-requests/{id}/reject", h.reject)
	mux.HandleFunc("POST /api/access-requests/{id}/resend-setup", h.resendSetup)
	mux.HandleFunc("POST /api/auth/complete-password-setup", h.completeSetup)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if !h.auth.ValidatePublicMutation(w, r) {
		return
	}
	var input struct {
		InstitutionName string    `json:"institutionName"`
		DisplayName     string    `json:"displayName"`
		Email           string    `json:"email"`
		RequestedRole   auth.Role `json:"requestedRole"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	err := h.service.Create(r.Context(), input.InstitutionName, input.DisplayName, input.Email, input.RequestedRole)
	if errors.Is(err, ErrDuplicate) {
		writeJSON(w, http.StatusAccepted, map[string]string{"message": "If this address is eligible, check its inbox for the next step"})
		return
	}
	if errors.Is(err, ErrPersonalEmail) || (err != nil && !strings.Contains(err.Error(), "send verification")) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		h.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "Check your institutional email to verify the request"})
}

func (h *Handler) verify(w http.ResponseWriter, r *http.Request) {
	if !h.auth.ValidatePublicMutation(w, r) {
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if err := h.service.Verify(r.Context(), input.Token); errors.Is(err, ErrInvalidToken) {
		writeError(w, http.StatusBadRequest, err.Error())
	} else if err != nil {
		h.internalError(w, err)
	} else {
		writeJSON(w, http.StatusOK, map[string]string{"message": "Email verified. Your request is awaiting approval"})
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.auth.RequireSuperadmin(w, r, false); !ok {
		return
	}
	requests, err := h.service.List(r.Context())
	if err != nil {
		h.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": requests})
}

func (h *Handler) approve(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.auth.RequireSuperadmin(w, r, true)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusNotFound, "Access request not found")
		return
	}
	var input struct {
		Role auth.Role `json:"role"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	err := h.service.Approve(r.Context(), id, actor.ID, input.Role)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
	} else if errors.Is(err, ErrInvalidState) || errors.Is(err, ErrDuplicate) {
		writeError(w, http.StatusConflict, err.Error())
	} else if err != nil {
		h.internalError(w, err)
	} else {
		writeJSON(w, http.StatusOK, map[string]string{"message": "Request approved and password setup email sent"})
	}
}

func (h *Handler) reject(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.auth.RequireSuperadmin(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Reason string `json:"reason"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	err := h.service.Reject(r.Context(), r.PathValue("id"), actor.ID, input.Reason)
	if errors.Is(err, ErrInvalidState) {
		writeError(w, http.StatusConflict, err.Error())
	} else if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
	} else {
		writeJSON(w, http.StatusOK, map[string]string{"message": "Request rejected"})
	}
}

func (h *Handler) resendSetup(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.auth.RequireSuperadmin(w, r, true); !ok {
		return
	}
	err := h.service.ResendSetup(r.Context(), r.PathValue("id"))
	if errors.Is(err, ErrInvalidState) {
		writeError(w, http.StatusConflict, err.Error())
	} else if err != nil {
		h.internalError(w, err)
	} else {
		writeJSON(w, http.StatusOK, map[string]string{"message": "Password setup email sent"})
	}
}

func (h *Handler) completeSetup(w http.ResponseWriter, r *http.Request) {
	if !h.auth.ValidatePublicMutation(w, r) {
		return
	}
	var input struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	err := h.service.CompleteSetup(r.Context(), input.Token, input.Password)
	if errors.Is(err, ErrInvalidToken) {
		writeError(w, http.StatusBadRequest, err.Error())
	} else if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
	} else {
		writeJSON(w, http.StatusOK, map[string]string{"message": "Password set. You can now sign in"})
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "Expected JSON")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "Expected one JSON object")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (h *Handler) internalError(w http.ResponseWriter, err error) {
	h.logger.Error("access request failed", slog.Any("error", err))
	writeError(w, http.StatusInternalServerError, "Internal server error")
}
