package auth

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Handler struct {
	store         *Store
	logger        *slog.Logger
	secure        bool
	allowedOrigin string
	dummyHash     string
	loginMutex    sync.Mutex
	attempts      map[string]loginAttempt
}

type loginAttempt struct {
	count int
	until time.Time
}

func NewHandler(store *Store, logger *slog.Logger, secure bool, allowedOrigin string) (*Handler, error) {
	hash, err := hashPassword("not-a-real-password")
	if err != nil {
		return nil, err
	}
	return &Handler{store: store, logger: logger, secure: secure, allowedOrigin: allowedOrigin, dummyHash: hash, attempts: make(map[string]loginAttempt)}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/login", h.login)
	mux.HandleFunc("GET /api/auth/me", h.me)
	mux.HandleFunc("POST /api/auth/logout", h.logout)
	mux.HandleFunc("POST /api/auth/change-password", h.changePassword)
	mux.HandleFunc("GET /api/users", h.listUsers)
	mux.HandleFunc("POST /api/users", h.createUser)
	mux.HandleFunc("PATCH /api/users/{id}", h.updateUser)
	mux.HandleFunc("POST /api/users/{id}/password", h.resetPassword)
}

func (h *Handler) RequireSuperadmin(w http.ResponseWriter, r *http.Request, mutation bool) (User, bool) {
	if mutation {
		current, ok := h.authorizeMutation(w, r, true)
		return current.user, ok
	}
	current, ok := h.requireSession(w, r)
	if !ok {
		return User{}, false
	}
	if current.user.Role != Superadmin {
		writeError(w, http.StatusForbidden, "Forbidden")
		return User{}, false
	}
	return current.user, true
}

func (h *Handler) ValidatePublicMutation(w http.ResponseWriter, r *http.Request) bool {
	return validMutationRequest(w, r, h.allowedOrigin)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	if !validMutationRequest(w, r, h.allowedOrigin) {
		return
	}
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	email, err := normalizeEmail(input.Email)
	if err != nil || input.Password == "" {
		writeError(w, http.StatusUnauthorized, "Invalid email or password")
		return
	}
	key := loginKey(r, email)
	if h.rateLimited(key) {
		writeError(w, http.StatusTooManyRequests, "Too many attempts. Try again later")
		return
	}
	user, hash, err := h.store.findUserByEmail(r.Context(), email)
	if errors.Is(err, pgx.ErrNoRows) {
		hash = h.dummyHash
	} else if err != nil {
		h.internalError(w, err)
		return
	}
	passwordValid := comparePassword(hash, input.Password) == nil
	if !passwordValid || !user.IsActive {
		h.recordFailure(key)
		writeError(w, http.StatusUnauthorized, "Invalid email or password")
		return
	}
	if user.MustChangePassword {
		writeError(w, http.StatusForbidden, "Complete password setup using your approval email")
		return
	}
	h.clearFailures(key)
	token, csrf, err := h.store.createSession(r.Context(), user.ID)
	if err != nil {
		h.internalError(w, err)
		return
	}
	http.SetCookie(w, sessionCookie(token, h.secure, int(sessionLifetime.Seconds())))
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "csrfToken": csrf})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	current, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"user": current.user, "csrfToken": current.csrfToken})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	current, ok := h.authorizeMutation(w, r, false)
	if !ok {
		return
	}
	if err := h.store.deleteSession(r.Context(), current.tokenHash); err != nil {
		h.internalError(w, err)
		return
	}
	http.SetCookie(w, sessionCookie("", h.secure, -1))
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	current, ok := h.authorizeMutation(w, r, false)
	if !ok {
		return
	}
	var input struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if err := validatePassword(input.NewPassword); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	err := h.store.changePassword(r.Context(), current.user.ID, input.CurrentPassword, input.NewPassword)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "Session expired")
		return
	}
	if errors.Is(err, errInvalidPassword) {
		writeError(w, http.StatusBadRequest, "Current password is incorrect")
		return
	}
	if err != nil {
		h.internalError(w, err)
		return
	}
	http.SetCookie(w, sessionCookie("", h.secure, -1))
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	current, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	if current.user.Role != Superadmin {
		writeError(w, http.StatusForbidden, "Forbidden")
		return
	}
	users, err := h.store.listUsers(r.Context())
	if err != nil {
		h.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	current, ok := h.authorizeMutation(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Email       string `json:"email"`
		DisplayName string `json:"displayName"`
		Password    string `json:"password"`
		Role        Role   `json:"role"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	email, err := normalizeEmail(input.Email)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if err != nil || input.DisplayName == "" || len(input.DisplayName) > 100 || !validRole(input.Role) {
		writeError(w, http.StatusBadRequest, "Invalid account details")
		return
	}
	if err := validatePassword(input.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, err := h.store.createUser(r.Context(), current.user.ID, User{Email: email, DisplayName: input.DisplayName, Role: input.Role}, input.Password)
	if uniqueViolation(err) {
		writeError(w, http.StatusConflict, "Email is already in use")
		return
	}
	if err != nil {
		h.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	current, ok := h.authorizeMutation(w, r, true)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusNotFound, "User not found")
		return
	}
	var input struct {
		Role     *Role `json:"role"`
		IsActive *bool `json:"isActive"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if (input.Role == nil && input.IsActive == nil) || (input.Role != nil && !validRole(*input.Role)) {
		writeError(w, http.StatusBadRequest, "Invalid account update")
		return
	}
	user, err := h.store.updateUser(r.Context(), current.user.ID, id, input.Role, input.IsActive)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "User not found")
		return
	}
	if errors.Is(err, errLastSuperadmin) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		h.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	current, ok := h.authorizeMutation(w, r, true)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusNotFound, "User not found")
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if err := validatePassword(input.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	err := h.store.resetPassword(r.Context(), current.user.ID, id, input.Password)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "User not found")
		return
	}
	if err != nil {
		h.internalError(w, err)
		return
	}
	if current.user.ID == id {
		http.SetCookie(w, sessionCookie("", h.secure, -1))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (session, bool) {
	cookie, err := r.Cookie(cookieName(h.secure))
	if err != nil || len(cookie.Value) != 43 {
		writeError(w, http.StatusUnauthorized, "Authentication required")
		return session{}, false
	}
	current, err := h.store.getSession(r.Context(), cookie.Value)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "Authentication required")
		return session{}, false
	}
	if err != nil {
		h.internalError(w, err)
		return session{}, false
	}
	return current, true
}

func (h *Handler) authorizeMutation(w http.ResponseWriter, r *http.Request, superadmin bool) (session, bool) {
	current, ok := h.requireSession(w, r)
	if !ok {
		return session{}, false
	}
	if superadmin && current.user.Role != Superadmin {
		writeError(w, http.StatusForbidden, "Forbidden")
		return session{}, false
	}
	if !validMutationRequest(w, r, h.allowedOrigin) {
		return session{}, false
	}
	if !validCSRF(current.csrfToken, r.Header.Get("X-CSRF-Token")) {
		writeError(w, http.StatusForbidden, "Invalid request token")
		return session{}, false
	}
	return current, true
}

func validMutationRequest(w http.ResponseWriter, r *http.Request, allowedOrigin string) bool {
	if r.Header.Get("X-Requested-With") != "AudioSpeechVault" {
		writeError(w, http.StatusForbidden, "Invalid request origin")
		return false
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeError(w, http.StatusForbidden, "Invalid request origin")
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || (origin != allowedOrigin && parsed.Host != r.Host) || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			writeError(w, http.StatusForbidden, "Invalid request origin")
			return false
		}
	}
	return true
}

func readJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "Expected JSON")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
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
	h.logger.Error("authentication request failed", slog.Any("error", err))
	writeError(w, http.StatusInternalServerError, "Internal server error")
}

func uniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func loginKey(r *http.Request, email string) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	return ip + ":" + email
}

func (h *Handler) rateLimited(key string) bool {
	h.loginMutex.Lock()
	defer h.loginMutex.Unlock()
	attempt := h.attempts[key]
	return attempt.count >= 5 && time.Now().Before(attempt.until)
}

func (h *Handler) recordFailure(key string) {
	h.loginMutex.Lock()
	defer h.loginMutex.Unlock()
	if len(h.attempts) > 10000 {
		h.attempts = make(map[string]loginAttempt)
	}
	attempt := h.attempts[key]
	if time.Now().After(attempt.until) {
		attempt = loginAttempt{until: time.Now().Add(15 * time.Minute)}
	}
	attempt.count++
	h.attempts[key] = attempt
}

func (h *Handler) clearFailures(key string) {
	h.loginMutex.Lock()
	defer h.loginMutex.Unlock()
	delete(h.attempts, key)
}
