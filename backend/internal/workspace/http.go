package workspace

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shivam3746/audio-speech-vault/internal/auth"
)

type Handler struct {
	store   *Store
	auth    *auth.Handler
	logger  *slog.Logger
	dataDir string
}

func NewHandler(store *Store, authHandler *auth.Handler, logger *slog.Logger, dataDir string) *Handler {
	return &Handler{store: store, auth: authHandler, logger: logger, dataDir: dataDir}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects", h.listProjects)
	mux.HandleFunc("POST /api/projects", h.createProject)
	mux.HandleFunc("POST /api/projects/{id}/members", h.addMember)
	mux.HandleFunc("GET /api/projects/{id}/people", h.listPeople)
	mux.HandleFunc("GET /api/projects/{id}/tiers", h.listTiers)
	mux.HandleFunc("POST /api/projects/{id}/tiers", h.createTier)
	mux.HandleFunc("GET /api/projects/{id}/recordings", h.listRecordings)
	mux.HandleFunc("POST /api/projects/{id}/recordings/upload", h.uploadRecording)
	mux.HandleFunc("GET /api/recordings/{id}/workspace", h.getWorkspace)
	mux.HandleFunc("GET /api/recordings/{id}/media", h.getMedia)
	mux.HandleFunc("POST /api/recordings/{id}/annotations", h.createAnnotation)
	mux.HandleFunc("PATCH /api/recordings/{id}/annotations/{annotationId}", h.updateAnnotation)
	mux.HandleFunc("DELETE /api/recordings/{id}/annotations/{annotationId}", h.deleteAnnotation)
	mux.HandleFunc("POST /api/recordings/{id}/annotations/{annotationId}/restore", h.restoreAnnotation)
	mux.HandleFunc("POST /api/recordings/{id}/submit", h.submit)
	mux.HandleFunc("POST /api/recordings/{id}/review", h.review)
}
func (h *Handler) listPeople(w http.ResponseWriter, r *http.Request) {
	user, ok := h.auth.Authorize(w, r, false, auth.Superadmin, auth.Admin)
	if !ok {
		return
	}
	if !validID(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "Project not found")
		return
	}
	items, err := h.store.ListPeople(r.Context(), user, r.PathValue("id"))
	h.respond(w, items, err)
}

func (h *Handler) user(w http.ResponseWriter, r *http.Request, mutation bool) (auth.User, bool) {
	return h.auth.Authorize(w, r, mutation, auth.Superadmin, auth.Admin, auth.Researcher, auth.Reviewer)
}
func validID(value string) bool { _, err := uuid.Parse(value); return err == nil }

func (h *Handler) listProjects(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r, false)
	if !ok {
		return
	}
	items, err := h.store.ListProjects(r.Context(), user)
	h.respond(w, items, err)
}
func (h *Handler) createProject(w http.ResponseWriter, r *http.Request) {
	user, ok := h.auth.Authorize(w, r, true, auth.Superadmin, auth.Admin)
	if !ok {
		return
	}
	var in struct{ Code, Name, Description string }
	if !readJSON(w, r, &in) {
		return
	}
	item, err := h.store.CreateProject(r.Context(), user, in.Code, in.Name, in.Description)
	h.respondStatus(w, item, err, http.StatusCreated)
}
func (h *Handler) addMember(w http.ResponseWriter, r *http.Request) {
	user, ok := h.auth.Authorize(w, r, true, auth.Superadmin, auth.Admin)
	if !ok {
		return
	}
	if !validID(r.PathValue("id")) {
		writeError(w, 404, "Project not found")
		return
	}
	var in struct {
		UserID         string    `json:"userId"`
		Responsibility auth.Role `json:"responsibility"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !validID(in.UserID) {
		writeError(w, 400, "Invalid member")
		return
	}
	err := h.store.AddMember(r.Context(), user, r.PathValue("id"), in.UserID, in.Responsibility)
	h.respondStatus(w, map[string]string{"message": "Member assigned"}, err, 200)
}
func (h *Handler) listTiers(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r, false)
	if !ok {
		return
	}
	if !validID(r.PathValue("id")) {
		writeError(w, 404, "Project not found")
		return
	}
	items, err := h.store.ListTiers(r.Context(), user, r.PathValue("id"))
	h.respond(w, items, err)
}
func (h *Handler) createTier(w http.ResponseWriter, r *http.Request) {
	user, ok := h.auth.Authorize(w, r, true, auth.Superadmin, auth.Admin)
	if !ok {
		return
	}
	if !validID(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "Project not found")
		return
	}
	var item Tier
	if !readJSON(w, r, &item) {
		return
	}
	created, err := h.store.CreateTier(r.Context(), user, r.PathValue("id"), item)
	h.respondStatus(w, created, err, 201)
}
func (h *Handler) listRecordings(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r, false)
	if !ok {
		return
	}
	if !validID(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "Project not found")
		return
	}
	items, err := h.store.ListRecordings(r.Context(), user, r.PathValue("id"))
	h.respond(w, items, err)
}
func (h *Handler) uploadRecording(w http.ResponseWriter, r *http.Request) {
	user, ok := h.auth.Authorize(w, r, true, auth.Superadmin, auth.Admin)
	if !ok {
		return
	}
	projectID := r.PathValue("id")
	if !validID(projectID) {
		writeError(w, http.StatusNotFound, "Project not found")
		return
	}
	const maxUpload int64 = 500 * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+1024*1024)
	if err := r.ParseMultipartForm(8 * 1024 * 1024); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid upload or file exceeds 500 MB")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("audio")
	if err != nil {
		writeError(w, http.StatusBadRequest, "An audio file is required")
		return
	}
	defer file.Close()
	if header.Size <= 0 || header.Size > maxUpload {
		writeError(w, http.StatusBadRequest, "Audio file must be between 1 byte and 500 MB")
		return
	}
	ext := strings.ToLower(filepath.Ext(filepath.Base(header.Filename)))
	expected := map[string]string{".mp3": "audio/mpeg", ".wav": "audio/wav", ".flac": "audio/flac", ".ogg": "audio/ogg", ".webm": "audio/webm"}
	mediaType, ok := expected[ext]
	if !ok {
		writeError(w, http.StatusBadRequest, "Unsupported audio file type")
		return
	}
	probe := make([]byte, 512)
	n, readErr := io.ReadFull(file, probe)
	if readErr != nil && readErr != io.ErrUnexpectedEOF {
		h.internal(w, readErr)
		return
	}
	if !validAudioContent(ext, probe[:n]) {
		writeError(w, http.StatusBadRequest, "File contents do not match the selected audio format")
		return
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		h.internal(w, err)
		return
	}
	durationMS, err := strconv.ParseInt(r.FormValue("durationMs"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Valid audio duration is required")
		return
	}
	optionalID := func(name string) (*string, error) {
		value := strings.TrimSpace(r.FormValue(name))
		if value == "" {
			return nil, nil
		}
		if !validID(value) {
			return nil, ErrInvalid
		}
		return &value, nil
	}
	researcherID, err := optionalID("researcherId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid researcher")
		return
	}
	reviewerID, err := optionalID("reviewerId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid reviewer")
		return
	}
	audioDir := filepath.Join(h.dataDir, "audio")
	if err = os.MkdirAll(audioDir, 0o700); err != nil {
		h.internal(w, err)
		return
	}
	storageKey := uuid.NewString() + ext
	destination := filepath.Join(audioDir, storageKey)
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		h.internal(w, err)
		return
	}
	written, copyErr := io.Copy(output, file)
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil || written != header.Size {
		_ = os.Remove(destination)
		if copyErr != nil {
			h.internal(w, copyErr)
		} else if closeErr != nil {
			h.internal(w, closeErr)
		} else {
			writeError(w, http.StatusBadRequest, "Incomplete audio upload")
		}
		return
	}
	item := Recording{Filename: filepath.Base(header.Filename), MediaType: mediaType, SizeBytes: written, DurationMS: durationMS, ResearcherID: researcherID, ReviewerID: reviewerID, StorageKey: storageKey}
	created, err := h.store.CreateRecording(r.Context(), user, projectID, item)
	if err != nil {
		_ = os.Remove(destination)
		h.respondStatus(w, created, err, http.StatusCreated)
		return
	}
	h.respondStatus(w, created, nil, http.StatusCreated)
}

func validAudioContent(extension string, contents []byte) bool {
	switch extension {
	case ".mp3":
		return len(contents) >= 3 && string(contents[:3]) == "ID3" || len(contents) >= 2 && contents[0] == 0xff && contents[1]&0xe0 == 0xe0
	case ".wav":
		return len(contents) >= 12 && string(contents[:4]) == "RIFF" && string(contents[8:12]) == "WAVE"
	case ".flac":
		return len(contents) >= 4 && string(contents[:4]) == "fLaC"
	case ".ogg":
		return len(contents) >= 4 && string(contents[:4]) == "OggS"
	case ".webm":
		return len(contents) >= 4 && contents[0] == 0x1a && contents[1] == 0x45 && contents[2] == 0xdf && contents[3] == 0xa3
	default:
		return false
	}
}
func (h *Handler) getMedia(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r, false)
	if !ok {
		return
	}
	if !validID(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "Recording not found")
		return
	}
	path, mediaType, err := h.store.MediaPath(r.Context(), user, r.PathValue("id"), filepath.Join(h.dataDir, "audio"))
	if err != nil {
		h.respond(w, nil, err)
		return
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "Audio file not found")
		return
	}
	if err != nil {
		h.internal(w, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		h.internal(w, err)
		return
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, "", info.ModTime(), file)
}
func (h *Handler) getWorkspace(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r, false)
	if !ok {
		return
	}
	if !validID(r.PathValue("id")) {
		writeError(w, 404, "Recording not found")
		return
	}
	item, err := h.store.GetWorkspace(r.Context(), user, r.PathValue("id"))
	h.respond(w, item, err)
}
func (h *Handler) createAnnotation(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r, true)
	if !ok {
		return
	}
	if !validID(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "Recording not found")
		return
	}
	var item Annotation
	if !readJSON(w, r, &item) {
		return
	}
	item.RecordingID = r.PathValue("id")
	created, err := h.store.SaveAnnotation(r.Context(), user, item, true)
	h.respondStatus(w, created, err, 201)
}
func (h *Handler) updateAnnotation(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r, true)
	if !ok {
		return
	}
	if !validID(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "Recording not found")
		return
	}
	var item Annotation
	if !readJSON(w, r, &item) {
		return
	}
	item.ID = r.PathValue("annotationId")
	item.RecordingID = r.PathValue("id")
	if !validID(item.ID) {
		writeError(w, 404, "Annotation not found")
		return
	}
	updated, err := h.store.SaveAnnotation(r.Context(), user, item, false)
	h.respond(w, updated, err)
}
func (h *Handler) deleteAnnotation(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r, true)
	if !ok {
		return
	}
	if !validID(r.PathValue("id")) || !validID(r.PathValue("annotationId")) {
		writeError(w, 404, "Annotation not found")
		return
	}
	var in struct {
		Version int `json:"version"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	item, err := h.store.SetAnnotationDeleted(r.Context(), user, r.PathValue("id"), r.PathValue("annotationId"), in.Version, true)
	h.respond(w, item, err)
}
func (h *Handler) restoreAnnotation(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r, true)
	if !ok {
		return
	}
	if !validID(r.PathValue("id")) || !validID(r.PathValue("annotationId")) {
		writeError(w, 404, "Annotation not found")
		return
	}
	var in struct {
		Version int `json:"version"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	item, err := h.store.SetAnnotationDeleted(r.Context(), user, r.PathValue("id"), r.PathValue("annotationId"), in.Version, false)
	h.respond(w, item, err)
}
func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r, true)
	if !ok {
		return
	}
	if !validID(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "Recording not found")
		return
	}
	err := h.store.Submit(r.Context(), user, r.PathValue("id"))
	h.respond(w, map[string]string{"message": "Recording submitted for review"}, err)
}
func (h *Handler) review(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r, true)
	if !ok {
		return
	}
	if !validID(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "Recording not found")
		return
	}
	var in struct{ Decision, Comment string }
	if !readJSON(w, r, &in) {
		return
	}
	err := h.store.Review(r.Context(), user, r.PathValue("id"), in.Decision, in.Comment)
	h.respond(w, map[string]string{"message": "Review recorded"}, err)
}

func (h *Handler) respond(w http.ResponseWriter, value any, err error) {
	h.respondStatus(w, value, err, 200)
}
func (h *Handler) respondStatus(w http.ResponseWriter, value any, err error, status int) {
	if err == nil {
		writeJSON(w, status, value)
		return
	}
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, ErrInvalid):
		writeError(w, 400, err.Error())
	case errors.Is(err, ErrDenied):
		writeError(w, 403, "Forbidden")
	case errors.Is(err, ErrNotFound):
		writeError(w, 404, err.Error())
	case errors.Is(err, ErrConflict):
		writeError(w, 409, err.Error())
	case errors.As(err, &pgErr) && pgErr.Code == "23505":
		writeError(w, 409, "A workspace item with that value already exists")
	default:
		h.logger.Error("workspace request failed", slog.Any("error", err))
		writeError(w, 500, "Internal server error")
	}
}
func (h *Handler) internal(w http.ResponseWriter, err error) {
	h.logger.Error("workspace request failed", slog.Any("error", err))
	writeError(w, http.StatusInternalServerError, "Internal server error")
}
func readJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, 415, "Expected JSON")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, 400, "Invalid JSON")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, 400, "Expected one JSON object")
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
