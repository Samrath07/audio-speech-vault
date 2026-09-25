package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shivam3746/audio-speech-vault/internal/auth"
)

type Handler struct {
	db      *pgxpool.Pool
	auth    *auth.Handler
	logger  *slog.Logger
	dataDir string
}

type Status struct {
	JobID            string         `json:"jobId"`
	Status           string         `json:"status"`
	AlgorithmVersion string         `json:"algorithmVersion"`
	Attempts         int            `json:"attempts"`
	ErrorCode        *string        `json:"errorCode,omitempty"`
	DurationMS       *int64         `json:"durationMs,omitempty"`
	PauseCount       *int           `json:"pauseCount,omitempty"`
	SilenceMS        *int64         `json:"silenceMs,omitempty"`
	RhythmVector     []float64      `json:"rhythmVector,omitempty"`
	PriorityScore    *float64       `json:"priorityScore,omitempty"`
	PriorityDetails  map[string]any `json:"priorityBreakdown,omitempty"`
	Configuration    AnalysisConfig `json:"configuration"`
}

type SignalEvent struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	StartMS     int64          `json:"startMs"`
	EndMS       int64          `json:"endMs"`
	Score       *float64       `json:"score,omitempty"`
	Payload     map[string]any `json:"payload"`
	ReviewState string         `json:"reviewState"`
	Comment     *string        `json:"comment,omitempty"`
}

func NewHandler(db *pgxpool.Pool, authHandler *auth.Handler, logger *slog.Logger, dataDir string) *Handler {
	return &Handler{db: db, auth: authHandler, logger: logger, dataDir: dataDir}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/recordings/{id}/analysis", h.status)
	mux.HandleFunc("POST /api/recordings/{id}/analysis/retry", h.retry)
	mux.HandleFunc("POST /api/recordings/{id}/analysis/cancel", h.cancel)
	mux.HandleFunc("GET /api/recordings/{id}/signal-events", h.events)
	mux.HandleFunc("POST /api/recordings/{id}/signal-events/{eventId}/review", h.reviewEvent)
	mux.HandleFunc("GET /api/recordings/{id}/similar", h.similar)
	mux.HandleFunc("GET /api/recordings/{id}/waveform", h.waveform)
	mux.HandleFunc("GET /api/projects/{id}/exports/summary.csv", h.exportSummary)
	mux.HandleFunc("GET /api/projects/{id}/exports/events.csv", h.exportEvents)
	mux.HandleFunc("GET /api/projects/{id}/exports/gold.csv", h.exportGold)
	mux.HandleFunc("GET /api/projects/{id}/exports/batch.zip", h.exportBatch)
	mux.HandleFunc("GET /api/projects/{id}/participant-roles", h.listParticipantRoles)
	mux.HandleFunc("POST /api/projects/{id}/participant-roles", h.createParticipantRole)
	mux.HandleFunc("GET /api/recordings/{id}/speaker-mappings", h.listSpeakerMappings)
	mux.HandleFunc("PUT /api/recordings/{id}/speaker-mappings", h.setSpeakerMapping)
	mux.HandleFunc("GET /api/projects/{id}/analysis-profile", h.getAnalysisProfile)
	mux.HandleFunc("PUT /api/projects/{id}/analysis-profile", h.updateAnalysisProfile)
	mux.HandleFunc("GET /api/projects/{id}/analysis-profile/history", h.analysisProfileHistory)
	mux.HandleFunc("GET /api/projects/{id}/export-history", h.exportHistory)
}

func (h *Handler) waveform(w http.ResponseWriter, r *http.Request) {
	user, ok := h.auth.Authorize(w, r, false, auth.Superadmin, auth.Admin, auth.Researcher, auth.Reviewer)
	if !ok || !validID(r.PathValue("id")) {
		if ok {
			writeError(w, 404, "Recording not found")
		}
		return
	}
	if allowed, err := h.canAccess(r.Context(), user, r.PathValue("id"), false); err != nil || !allowed {
		h.accessError(w, err)
		return
	}
	var durationMS int64
	var encoded []byte
	err := h.db.QueryRow(r.Context(), `SELECT duration_ms,loudness_envelope FROM recording_analyses WHERE recording_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, r.PathValue("id")).Scan(&durationMS, &encoded)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "Waveform analysis not ready")
		return
	}
	if err != nil {
		h.internal(w, err)
		return
	}
	var envelope []EnvelopePoint
	if err = json.Unmarshal(encoded, &envelope); err != nil {
		h.internal(w, err)
		return
	}
	peaks := make([]float64, len(envelope))
	for index, point := range envelope {
		peaks[index] = round(math.Min(1, math.Pow(10, point.DBFS/20)))
	}
	writeJSON(w, 200, map[string]any{"duration": float64(durationMS) / 1000, "peaks": [][]float64{peaks}})
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	user, ok := h.auth.Authorize(w, r, false, auth.Superadmin, auth.Admin, auth.Researcher, auth.Reviewer)
	if !ok || !validID(r.PathValue("id")) {
		if ok {
			writeError(w, http.StatusNotFound, "Recording not found")
		}
		return
	}
	if allowed, err := h.canAccess(r.Context(), user, r.PathValue("id"), false); err != nil || !allowed {
		h.accessError(w, err)
		return
	}
	var result Status
	var rhythm, priorityDetails, configuration []byte
	err := h.db.QueryRow(r.Context(), `SELECT j.id,j.status,j.algorithm_version,j.attempts,j.error_code,a.duration_ms,a.pause_count,a.silence_ms,COALESCE(a.rhythm_vector,'[]'),a.priority_score,COALESCE(a.priority_breakdown,'{}'),COALESCE(a.configuration,j.configuration,'{}')
		FROM analysis_jobs j LEFT JOIN recording_analyses a ON a.job_id=j.id WHERE j.recording_id=$1 ORDER BY j.created_at DESC,j.id DESC LIMIT 1`, r.PathValue("id")).
		Scan(&result.JobID, &result.Status, &result.AlgorithmVersion, &result.Attempts, &result.ErrorCode, &result.DurationMS, &result.PauseCount, &result.SilenceMS, &rhythm, &result.PriorityScore, &priorityDetails, &configuration)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Analysis job not found")
		return
	}
	if err != nil {
		h.internal(w, err)
		return
	}
	_ = json.Unmarshal(rhythm, &result.RhythmVector)
	_ = json.Unmarshal(priorityDetails, &result.PriorityDetails)
	_ = json.Unmarshal(configuration, &result.Configuration)
	writeJSON(w, http.StatusOK, result)
}

type SimilarRecording struct {
	ID            string  `json:"id"`
	Filename      string  `json:"filename"`
	Distance      float64 `json:"distance"`
	PriorityScore float64 `json:"priorityScore"`
}

func (h *Handler) similar(w http.ResponseWriter, r *http.Request) {
	user, ok := h.auth.Authorize(w, r, false, auth.Superadmin, auth.Admin, auth.Researcher, auth.Reviewer)
	if !ok || !validID(r.PathValue("id")) {
		if ok {
			writeError(w, 404, "Recording not found")
		}
		return
	}
	if allowed, err := h.canAccess(r.Context(), user, r.PathValue("id"), false); err != nil || !allowed {
		h.accessError(w, err)
		return
	}
	var projectID string
	var sourceJSON []byte
	if err := h.db.QueryRow(r.Context(), `SELECT r.project_id,a.rhythm_vector FROM recordings r JOIN LATERAL (SELECT rhythm_vector FROM recording_analyses WHERE recording_id=r.id ORDER BY created_at DESC,id DESC LIMIT 1) a ON true WHERE r.id=$1`, r.PathValue("id")).Scan(&projectID, &sourceJSON); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, 404, "Completed analysis not found")
			return
		}
		h.internal(w, err)
		return
	}
	var source []float64
	_ = json.Unmarshal(sourceJSON, &source)
	rows, err := h.db.Query(r.Context(), `SELECT r.id,r.original_filename,a.rhythm_vector,a.priority_score FROM recordings r JOIN LATERAL (SELECT rhythm_vector,priority_score FROM recording_analyses WHERE recording_id=r.id ORDER BY created_at DESC,id DESC LIMIT 1) a ON true WHERE r.project_id=$1 AND r.id<>$2`, projectID, r.PathValue("id"))
	if err != nil {
		h.internal(w, err)
		return
	}
	defer rows.Close()
	items := []SimilarRecording{}
	for rows.Next() {
		var item SimilarRecording
		var encoded []byte
		if err = rows.Scan(&item.ID, &item.Filename, &encoded, &item.PriorityScore); err != nil {
			h.internal(w, err)
			return
		}
		var vector []float64
		_ = json.Unmarshal(encoded, &vector)
		item.Distance = round(vectorDistance(source, vector))
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		h.internal(w, err)
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Distance < items[j].Distance })
	if len(items) > 20 {
		items = items[:20]
	}
	writeJSON(w, 200, items)
}

func vectorDistance(left, right []float64) float64 {
	if len(left) == 0 || len(left) != len(right) {
		return math.Inf(1)
	}
	scales := []float64{1, 20, 30, 30}
	var sum float64
	for i := range left {
		scale := 1.0
		if i < len(scales) {
			scale = scales[i]
		}
		delta := (left[i] - right[i]) / scale
		sum += delta * delta
	}
	return math.Sqrt(sum)
}

func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	user, ok := h.auth.Authorize(w, r, false, auth.Superadmin, auth.Admin, auth.Researcher, auth.Reviewer)
	if !ok || !validID(r.PathValue("id")) {
		if ok {
			writeError(w, http.StatusNotFound, "Recording not found")
		}
		return
	}
	if allowed, err := h.canAccess(r.Context(), user, r.PathValue("id"), false); err != nil || !allowed {
		h.accessError(w, err)
		return
	}
	eventType := strings.TrimSpace(r.URL.Query().Get("type"))
	reviewState := strings.TrimSpace(r.URL.Query().Get("reviewState"))
	validType := map[string]bool{"": true, "pause": true, "long_pause": true, "repetition_candidate": true, "speaker_turn": true, "speaker_a": true, "speaker_b": true}
	validReview := map[string]bool{"": true, "unreviewed": true, "confirmed": true, "rejected": true, "uncertain": true}
	if !validType[eventType] || !validReview[reviewState] {
		writeError(w, http.StatusBadRequest, "Invalid event filter")
		return
	}
	rows, err := h.db.Query(r.Context(), `SELECT id,event_type,start_ms,end_ms,score,payload,review_state,review_comment FROM signal_events
		WHERE recording_id=$1 AND analysis_id=(SELECT id FROM recording_analyses WHERE recording_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1) AND ($2='' OR event_type=$2) AND ($3='' OR review_state=$3) ORDER BY start_ms,end_ms,id LIMIT 5000`, r.PathValue("id"), eventType, reviewState)
	if err != nil {
		h.internal(w, err)
		return
	}
	defer rows.Close()
	items := []SignalEvent{}
	for rows.Next() {
		var item SignalEvent
		var payload []byte
		if err = rows.Scan(&item.ID, &item.Type, &item.StartMS, &item.EndMS, &item.Score, &payload, &item.ReviewState, &item.Comment); err != nil {
			h.internal(w, err)
			return
		}
		_ = json.Unmarshal(payload, &item.Payload)
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		h.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) retry(w http.ResponseWriter, r *http.Request) {
	user, ok := h.auth.Authorize(w, r, true, auth.Superadmin, auth.Admin)
	if !ok || !validID(r.PathValue("id")) {
		if ok {
			writeError(w, http.StatusNotFound, "Recording not found")
		}
		return
	}
	if allowed, err := h.canAccess(r.Context(), user, r.PathValue("id"), true); err != nil || !allowed {
		h.accessError(w, err)
		return
	}
	command, err := h.db.Exec(r.Context(), `UPDATE analysis_jobs SET status='queued',attempts=0,available_at=NOW(),started_at=NULL,completed_at=NULL,error_code=NULL,error_message=NULL,updated_at=NOW() WHERE id=(SELECT id FROM analysis_jobs WHERE recording_id=$1 AND status IN ('failed','completed') ORDER BY created_at DESC,id DESC LIMIT 1)`, r.PathValue("id"))
	if err != nil {
		h.internal(w, err)
		return
	}
	if command.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "Analysis is already queued or processing")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "Analysis queued"})
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	user, ok := h.auth.Authorize(w, r, true, auth.Superadmin, auth.Admin)
	if !ok || !validID(r.PathValue("id")) {
		if ok {
			writeError(w, http.StatusNotFound, "Recording not found")
		}
		return
	}
	if allowed, err := h.canAccess(r.Context(), user, r.PathValue("id"), true); err != nil || !allowed {
		h.accessError(w, err)
		return
	}
	command, err := h.db.Exec(r.Context(), `UPDATE analysis_jobs SET status='cancelled',completed_at=NOW(),updated_at=NOW() WHERE id=(SELECT id FROM analysis_jobs WHERE recording_id=$1 AND status='queued' ORDER BY created_at DESC,id DESC LIMIT 1)`, r.PathValue("id"))
	if err != nil {
		h.internal(w, err)
		return
	}
	if command.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "Only queued analysis can be cancelled")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Analysis cancelled"})
}

func (h *Handler) reviewEvent(w http.ResponseWriter, r *http.Request) {
	user, ok := h.auth.Authorize(w, r, true, auth.Superadmin, auth.Admin, auth.Researcher, auth.Reviewer)
	if !ok || !validID(r.PathValue("id")) || !validID(r.PathValue("eventId")) {
		if ok {
			writeError(w, http.StatusNotFound, "Signal event not found")
		}
		return
	}
	if allowed, err := h.canAccess(r.Context(), user, r.PathValue("id"), false); err != nil || !allowed {
		h.accessError(w, err)
		return
	}
	var in struct {
		State   string `json:"state"`
		Comment string `json:"comment"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.State != "confirmed" && in.State != "rejected" && in.State != "uncertain" {
		writeError(w, http.StatusBadRequest, "Invalid review decision")
		return
	}
	in.Comment = strings.TrimSpace(in.Comment)
	if len(in.Comment) > 2000 {
		writeError(w, http.StatusBadRequest, "Review comment is too long")
		return
	}
	command, err := h.db.Exec(r.Context(), `UPDATE signal_events SET review_state=$1,reviewed_by=$2,reviewed_at=NOW(),review_comment=NULLIF($3,'') WHERE id=$4 AND recording_id=$5`, in.State, user.ID, in.Comment, r.PathValue("eventId"), r.PathValue("id"))
	if err != nil {
		h.internal(w, err)
		return
	}
	if command.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "Signal event not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Signal event reviewed"})
}

func (h *Handler) canAccess(ctx context.Context, user auth.User, recordingID string, manage bool) (bool, error) {
	if user.Role == auth.Superadmin {
		return true, nil
	}
	if manage && user.Role != auth.Admin {
		return false, nil
	}
	var allowed bool
	err := h.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM recordings r JOIN projects p ON p.id=r.project_id JOIN users u ON u.id=$2 LEFT JOIN project_members pm ON pm.project_id=p.id AND pm.user_id=$2 LEFT JOIN annotation_tasks t ON t.recording_id=r.id
		WHERE r.id=$1 AND (($3='admin' AND (p.created_by=$2 OR (u.institution_id IS NOT NULL AND p.institution_id=u.institution_id))) OR (pm.user_id=$2 AND (t.assigned_researcher_id=$2 OR t.assigned_reviewer_id=$2))))`, recordingID, user.ID, user.Role).Scan(&allowed)
	return allowed, err
}

func (h *Handler) accessError(w http.ResponseWriter, err error) {
	if err != nil {
		h.internal(w, err)
		return
	}
	writeError(w, http.StatusForbidden, "Forbidden")
}

func (h *Handler) internal(w http.ResponseWriter, err error) {
	h.logger.Error("analysis request failed", slog.Any("error", err))
	writeError(w, http.StatusInternalServerError, "Internal server error")
}

func validID(value string) bool { _, err := uuid.Parse(value); return err == nil }

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "Expected JSON")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
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
