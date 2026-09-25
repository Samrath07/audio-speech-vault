package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shivam3746/audio-speech-vault/internal/auth"
)

var participantCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,31}$`)

type ParticipantRole struct {
	ID           string `json:"id"`
	Code         string `json:"code"`
	Label        string `json:"label"`
	DisplayOrder int    `json:"displayOrder"`
}

type SpeakerMapping struct {
	SpeakerCluster    string `json:"speakerCluster"`
	ParticipantRoleID string `json:"participantRoleId"`
	ParticipantCode   string `json:"participantCode"`
	ParticipantLabel  string `json:"participantLabel"`
	MappedBy          string `json:"mappedBy"`
}

type AnalysisProfile struct {
	Version              int     `json:"version"`
	SilenceDBFS          float64 `json:"silenceDbfs"`
	MinimumPauseMS       int64   `json:"minimumPauseMs"`
	LongPauseMS          int64   `json:"longPauseMs"`
	RepetitionSimilarity float64 `json:"repetitionSimilarity"`
	SpeakerSeparation    float64 `json:"speakerSeparation"`
}

type ProfileHistory struct {
	AlgorithmVersion string         `json:"algorithmVersion"`
	Configuration    AnalysisConfig `json:"configuration"`
	Queued           int            `json:"queued"`
	Processing       int            `json:"processing"`
	Completed        int            `json:"completed"`
	Failed           int            `json:"failed"`
	CreatedAt        time.Time      `json:"createdAt"`
}

type ExportHistory struct {
	ID               string     `json:"id"`
	ExportType       string     `json:"exportType"`
	AlgorithmVersion string     `json:"algorithmVersion"`
	Status           string     `json:"status"`
	Checksum         *string    `json:"checksum,omitempty"`
	RequestedBy      string     `json:"requestedBy"`
	CreatedAt        time.Time  `json:"createdAt"`
	CompletedAt      *time.Time `json:"completedAt,omitempty"`
}

func (h *Handler) analysisProfileHistory(w http.ResponseWriter, r *http.Request) {
	user, ok := h.auth.Authorize(w, r, false, auth.Superadmin, auth.Admin, auth.Researcher, auth.Reviewer)
	if !ok || !validID(r.PathValue("id")) {
		if ok {
			writeError(w, 404, "Project not found")
		}
		return
	}
	allowed, _, err := h.canAccessProject(r.Context(), user, r.PathValue("id"))
	if err != nil || !allowed {
		h.accessError(w, err)
		return
	}
	rows, err := h.db.Query(r.Context(), `SELECT j.algorithm_version,j.configuration,count(*) FILTER(WHERE j.status='queued'),count(*) FILTER(WHERE j.status='processing'),count(*) FILTER(WHERE j.status='completed'),count(*) FILTER(WHERE j.status='failed'),min(j.created_at) FROM analysis_jobs j JOIN recordings r ON r.id=j.recording_id WHERE r.project_id=$1 GROUP BY j.algorithm_version,j.configuration ORDER BY min(j.created_at) DESC LIMIT 50`, r.PathValue("id"))
	if err != nil {
		h.internal(w, err)
		return
	}
	defer rows.Close()
	items := []ProfileHistory{}
	for rows.Next() {
		var item ProfileHistory
		var configuration []byte
		if err = rows.Scan(&item.AlgorithmVersion, &configuration, &item.Queued, &item.Processing, &item.Completed, &item.Failed, &item.CreatedAt); err != nil {
			h.internal(w, err)
			return
		}
		_ = json.Unmarshal(configuration, &item.Configuration)
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		h.internal(w, err)
		return
	}
	writeJSON(w, 200, items)
}

func (h *Handler) exportHistory(w http.ResponseWriter, r *http.Request) {
	user, ok := h.auth.Authorize(w, r, false, auth.Superadmin, auth.Admin)
	if !ok || !validID(r.PathValue("id")) {
		if ok {
			writeError(w, 404, "Project not found")
		}
		return
	}
	allowed, _, err := h.canAccessProject(r.Context(), user, r.PathValue("id"))
	if err != nil || !allowed {
		h.accessError(w, err)
		return
	}
	rows, err := h.db.Query(r.Context(), `SELECT e.id,e.export_type,e.algorithm_version,e.status,e.output_sha256,u.display_name,e.created_at,e.completed_at FROM export_audits e JOIN users u ON u.id=e.requested_by WHERE e.project_id=$1 ORDER BY e.created_at DESC LIMIT 100`, r.PathValue("id"))
	if err != nil {
		h.internal(w, err)
		return
	}
	defer rows.Close()
	items := []ExportHistory{}
	for rows.Next() {
		var item ExportHistory
		if err = rows.Scan(&item.ID, &item.ExportType, &item.AlgorithmVersion, &item.Status, &item.Checksum, &item.RequestedBy, &item.CreatedAt, &item.CompletedAt); err != nil {
			h.internal(w, err)
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		h.internal(w, err)
		return
	}
	writeJSON(w, 200, items)
}

func (h *Handler) getAnalysisProfile(w http.ResponseWriter, r *http.Request) {
	user, ok := h.auth.Authorize(w, r, false, auth.Superadmin, auth.Admin, auth.Researcher, auth.Reviewer)
	if !ok || !validID(r.PathValue("id")) {
		if ok {
			writeError(w, 404, "Project not found")
		}
		return
	}
	allowed, _, err := h.canAccessProject(r.Context(), user, r.PathValue("id"))
	if err != nil || !allowed {
		h.accessError(w, err)
		return
	}
	var profile AnalysisProfile
	err = h.db.QueryRow(r.Context(), `SELECT version,silence_dbfs,minimum_pause_ms,long_pause_ms,repetition_similarity,speaker_separation FROM analysis_profiles WHERE project_id=$1`, r.PathValue("id")).Scan(&profile.Version, &profile.SilenceDBFS, &profile.MinimumPauseMS, &profile.LongPauseMS, &profile.RepetitionSimilarity, &profile.SpeakerSeparation)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "Analysis profile not found")
		return
	}
	if err != nil {
		h.internal(w, err)
		return
	}
	writeJSON(w, 200, profile)
}

func (h *Handler) updateAnalysisProfile(w http.ResponseWriter, r *http.Request) {
	user, ok := h.auth.Authorize(w, r, true, auth.Superadmin, auth.Admin)
	if !ok || !validID(r.PathValue("id")) {
		if ok {
			writeError(w, 404, "Project not found")
		}
		return
	}
	allowed, _, err := h.canAccessProject(r.Context(), user, r.PathValue("id"))
	if err != nil || !allowed {
		h.accessError(w, err)
		return
	}
	var profile AnalysisProfile
	if !decodeJSON(w, r, &profile) {
		return
	}
	if profile.SilenceDBFS < -80 || profile.SilenceDBFS > -10 || profile.MinimumPauseMS < 100 || profile.MinimumPauseMS > 10000 || profile.LongPauseMS < 500 || profile.LongPauseMS > 60000 || profile.LongPauseMS < profile.MinimumPauseMS || profile.RepetitionSimilarity < .5 || profile.RepetitionSimilarity > .99 || profile.SpeakerSeparation < .1 || profile.SpeakerSeparation > 5 {
		writeError(w, 400, "Analysis profile values are outside the supported ranges")
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		h.internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	err = tx.QueryRow(r.Context(), `UPDATE analysis_profiles SET version=version+1,silence_dbfs=$2,minimum_pause_ms=$3,long_pause_ms=$4,repetition_similarity=$5,speaker_separation=$6,updated_by=$7,updated_at=NOW() WHERE project_id=$1 RETURNING version`, r.PathValue("id"), profile.SilenceDBFS, profile.MinimumPauseMS, profile.LongPauseMS, profile.RepetitionSimilarity, profile.SpeakerSeparation, user.ID).Scan(&profile.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "Analysis profile not found")
		return
	}
	if err != nil {
		h.internal(w, err)
		return
	}
	configuration := map[string]any{"silenceDbfs": profile.SilenceDBFS, "minimumPauseMs": profile.MinimumPauseMS, "longPauseMs": profile.LongPauseMS, "repetitionSimilarity": profile.RepetitionSimilarity, "speakerSeparation": profile.SpeakerSeparation}
	_, err = tx.Exec(r.Context(), `INSERT INTO analysis_jobs(recording_id,algorithm_version,configuration) SELECT id,$2,$3 FROM recordings WHERE project_id=$1 ON CONFLICT(recording_id,algorithm_version) DO NOTHING`, r.PathValue("id"), "signal-v3-p"+strconv.Itoa(profile.Version), configuration)
	if err != nil {
		h.internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		h.internal(w, err)
		return
	}
	writeJSON(w, 200, profile)
}

func (h *Handler) listParticipantRoles(w http.ResponseWriter, r *http.Request) {
	user, ok := h.auth.Authorize(w, r, false, auth.Superadmin, auth.Admin, auth.Researcher, auth.Reviewer)
	if !ok || !validID(r.PathValue("id")) {
		if ok {
			writeError(w, 404, "Project not found")
		}
		return
	}
	allowed, _, err := h.canAccessProject(r.Context(), user, r.PathValue("id"))
	if err != nil || !allowed {
		h.accessError(w, err)
		return
	}
	rows, err := h.db.Query(r.Context(), `SELECT id,code,label,display_order FROM project_participant_roles WHERE project_id=$1 AND is_active ORDER BY display_order,label`, r.PathValue("id"))
	if err != nil {
		h.internal(w, err)
		return
	}
	defer rows.Close()
	items := []ParticipantRole{}
	for rows.Next() {
		var item ParticipantRole
		if err = rows.Scan(&item.ID, &item.Code, &item.Label, &item.DisplayOrder); err != nil {
			h.internal(w, err)
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		h.internal(w, err)
		return
	}
	writeJSON(w, 200, items)
}

func (h *Handler) createParticipantRole(w http.ResponseWriter, r *http.Request) {
	user, ok := h.auth.Authorize(w, r, true, auth.Superadmin, auth.Admin)
	if !ok || !validID(r.PathValue("id")) {
		if ok {
			writeError(w, 404, "Project not found")
		}
		return
	}
	allowed, _, err := h.canAccessProject(r.Context(), user, r.PathValue("id"))
	if err != nil || !allowed {
		h.accessError(w, err)
		return
	}
	var in ParticipantRole
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Code = strings.ToLower(strings.TrimSpace(in.Code))
	in.Label = strings.TrimSpace(in.Label)
	if !participantCodePattern.MatchString(in.Code) || len(in.Label) == 0 || len(in.Label) > 80 || strings.ContainsAny(in.Label, "\r\n\x00") || in.DisplayOrder < 0 {
		writeError(w, 400, "Invalid participant role")
		return
	}
	err = h.db.QueryRow(r.Context(), `INSERT INTO project_participant_roles(project_id,code,label,display_order,created_by) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING RETURNING id`, r.PathValue("id"), in.Code, in.Label, in.DisplayOrder, user.ID).Scan(&in.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "A participant role with that code or label already exists")
		return
	}
	if err != nil {
		h.internal(w, err)
		return
	}
	writeJSON(w, 201, in)
}

func (h *Handler) listSpeakerMappings(w http.ResponseWriter, r *http.Request) {
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
	rows, err := h.db.Query(r.Context(), `SELECT m.speaker_cluster,m.participant_role_id,p.code,p.label,u.display_name FROM speaker_mappings m JOIN project_participant_roles p ON p.id=m.participant_role_id JOIN users u ON u.id=m.mapped_by WHERE m.recording_id=$1 AND m.analysis_id=(SELECT id FROM recording_analyses WHERE recording_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1) ORDER BY m.speaker_cluster`, r.PathValue("id"))
	if err != nil {
		h.internal(w, err)
		return
	}
	defer rows.Close()
	items := []SpeakerMapping{}
	for rows.Next() {
		var item SpeakerMapping
		if err = rows.Scan(&item.SpeakerCluster, &item.ParticipantRoleID, &item.ParticipantCode, &item.ParticipantLabel, &item.MappedBy); err != nil {
			h.internal(w, err)
			return
		}
		items = append(items, item)
	}
	writeJSON(w, 200, items)
}

func (h *Handler) setSpeakerMapping(w http.ResponseWriter, r *http.Request) {
	user, ok := h.auth.Authorize(w, r, true, auth.Superadmin, auth.Admin, auth.Reviewer)
	if !ok || !validID(r.PathValue("id")) {
		if ok {
			writeError(w, 404, "Recording not found")
		}
		return
	}
	allowed, err := h.canMapSpeakers(r.Context(), user, r.PathValue("id"))
	if err != nil || !allowed {
		h.accessError(w, err)
		return
	}
	var in struct {
		SpeakerCluster    string `json:"speakerCluster"`
		ParticipantRoleID string `json:"participantRoleId"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if (in.SpeakerCluster != "speaker_a" && in.SpeakerCluster != "speaker_b") || !validID(in.ParticipantRoleID) {
		writeError(w, 400, "Invalid speaker mapping")
		return
	}
	command, err := h.db.Exec(r.Context(), `INSERT INTO speaker_mappings(analysis_id,recording_id,speaker_cluster,participant_role_id,mapped_by) SELECT a.id,r.id,$2,p.id,$4 FROM recordings r JOIN LATERAL (SELECT id FROM recording_analyses WHERE recording_id=r.id ORDER BY created_at DESC,id DESC LIMIT 1) a ON true JOIN project_participant_roles p ON p.project_id=r.project_id AND p.id=$3 AND p.is_active WHERE r.id=$1 ON CONFLICT(analysis_id,speaker_cluster) DO UPDATE SET participant_role_id=EXCLUDED.participant_role_id,mapped_by=EXCLUDED.mapped_by,updated_at=NOW()`, r.PathValue("id"), in.SpeakerCluster, in.ParticipantRoleID, user.ID)
	if err != nil {
		h.internal(w, err)
		return
	}
	if command.RowsAffected() == 0 {
		writeError(w, 400, "Participant role does not belong to this project")
		return
	}
	writeJSON(w, 200, map[string]string{"message": "Speaker mapping saved"})
}

func (h *Handler) canMapSpeakers(ctx context.Context, user auth.User, recordingID string) (bool, error) {
	if user.Role == auth.Superadmin {
		return true, nil
	}
	var allowed bool
	err := h.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM recordings r JOIN projects p ON p.id=r.project_id JOIN users u ON u.id=$2 JOIN annotation_tasks t ON t.recording_id=r.id WHERE r.id=$1 AND (($3='admin' AND (p.created_by=$2 OR (u.institution_id IS NOT NULL AND p.institution_id=u.institution_id))) OR ($3='reviewer' AND t.assigned_reviewer_id=$2)))`, recordingID, user.ID, user.Role).Scan(&allowed)
	return allowed, err
}
