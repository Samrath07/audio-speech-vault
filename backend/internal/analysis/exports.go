package analysis

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shivam3746/audio-speech-vault/internal/auth"
)

func csvSafe(value string) string {
	if value != "" && strings.ContainsRune("=+-@", rune(value[0])) {
		return "'" + value
	}
	return value
}

func (h *Handler) exportSummary(w http.ResponseWriter, r *http.Request) {
	h.serveCSV(w, r, "summary", h.summaryCSV)
}
func (h *Handler) exportEvents(w http.ResponseWriter, r *http.Request) {
	h.serveCSV(w, r, "events", h.eventsCSV)
}
func (h *Handler) exportGold(w http.ResponseWriter, r *http.Request) {
	h.serveCSV(w, r, "gold", h.goldCSV)
}

type csvBuilder func(context.Context, string) ([]byte, error)

func (h *Handler) serveCSV(w http.ResponseWriter, r *http.Request, name string, build csvBuilder) {
	user, ok := h.auth.Authorize(w, r, false, auth.Superadmin, auth.Admin, auth.Researcher, auth.Reviewer)
	if !ok || !validID(r.PathValue("id")) {
		if ok {
			writeError(w, 404, "Project not found")
		}
		return
	}
	allowed, code, err := h.canAccessProject(r.Context(), user, r.PathValue("id"))
	if err != nil || !allowed {
		h.accessError(w, err)
		return
	}
	contents, err := build(r.Context(), r.PathValue("id"))
	if err != nil {
		h.internal(w, err)
		return
	}
	sum := sha256.Sum256(contents)
	exportType := map[string]string{"summary": "summary_csv", "events": "event_csv", "gold": "gold_csv"}[name]
	h.auditExport(r.Context(), r.PathValue("id"), user.ID, exportType, hex.EncodeToString(sum[:]))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.csv"`, code, name))
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(contents)
}

func (h *Handler) summaryCSV(ctx context.Context, projectID string) ([]byte, error) {
	rows, err := h.db.Query(ctx, `SELECT r.id,r.original_filename,a.audio_sha256,a.duration_ms,a.pause_count,a.silence_ms,a.priority_score,a.algorithm_version FROM recordings r JOIN LATERAL (SELECT * FROM recording_analyses WHERE recording_id=r.id ORDER BY created_at DESC,id DESC LIMIT 1) a ON true WHERE r.project_id=$1 ORDER BY a.priority_score DESC,r.original_filename`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	_ = writer.Write([]string{"recording_id", "filename", "sha256", "duration_ms", "pause_count", "silence_ms", "priority_score", "algorithm_version"})
	for rows.Next() {
		var id, name, hash, version string
		var duration, silence int64
		var pauses int
		var priority float64
		if err = rows.Scan(&id, &name, &hash, &duration, &pauses, &silence, &priority, &version); err != nil {
			return nil, err
		}
		_ = writer.Write([]string{id, csvSafe(name), hash, fmt.Sprint(duration), fmt.Sprint(pauses), fmt.Sprint(silence), fmt.Sprintf("%.4f", priority), version})
	}
	writer.Flush()
	if err = writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), rows.Err()
}

func (h *Handler) eventsCSV(ctx context.Context, projectID string) ([]byte, error) {
	rows, err := h.db.Query(ctx, `SELECT e.recording_id,r.original_filename,e.id,e.event_type,e.start_ms,e.end_ms,e.score,e.review_state,COALESCE(e.review_comment,''),a.algorithm_version FROM recordings r JOIN LATERAL (SELECT id,algorithm_version FROM recording_analyses WHERE recording_id=r.id ORDER BY created_at DESC,id DESC LIMIT 1) a ON true JOIN signal_events e ON e.analysis_id=a.id WHERE r.project_id=$1 ORDER BY r.original_filename,e.start_ms,e.id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	_ = writer.Write([]string{"recording_id", "filename", "event_id", "event_type", "start_ms", "end_ms", "score", "review_state", "review_comment", "algorithm_version"})
	for rows.Next() {
		var recordingID, name, eventID, eventType, state, comment, version string
		var start, end int64
		var score *float64
		if err = rows.Scan(&recordingID, &name, &eventID, &eventType, &start, &end, &score, &state, &comment, &version); err != nil {
			return nil, err
		}
		scoreText := ""
		if score != nil {
			scoreText = fmt.Sprintf("%.4f", *score)
		}
		_ = writer.Write([]string{recordingID, csvSafe(name), eventID, eventType, fmt.Sprint(start), fmt.Sprint(end), scoreText, state, csvSafe(comment), version})
	}
	writer.Flush()
	if err = writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), rows.Err()
}

func (h *Handler) goldCSV(ctx context.Context, projectID string) ([]byte, error) {
	rows, err := h.db.Query(ctx, `SELECT a.recording_id,r.original_filename,a.tier_id,t.name,a.start_ms,a.end_ms,a.value,a.version,a.updated_at FROM annotations a JOIN recordings r ON r.id=a.recording_id JOIN annotation_tasks task ON task.recording_id=r.id JOIN tier_definitions t ON t.id=a.tier_id WHERE r.project_id=$1 AND task.status='approved' AND a.deleted_at IS NULL ORDER BY r.original_filename,a.start_ms,a.id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	_ = writer.Write([]string{"recording_id", "filename", "tier_id", "tier_name", "start_ms", "end_ms", "value", "annotation_version", "review_status", "exported_at"})
	for rows.Next() {
		var recordingID, name, tierID, tierName, value string
		var start, end int64
		var version int
		var updated time.Time
		if err = rows.Scan(&recordingID, &name, &tierID, &tierName, &start, &end, &value, &version, &updated); err != nil {
			return nil, err
		}
		_ = writer.Write([]string{recordingID, csvSafe(name), tierID, csvSafe(tierName), fmt.Sprint(start), fmt.Sprint(end), csvSafe(value), fmt.Sprint(version), "approved", updated.UTC().Format(time.RFC3339)})
	}
	writer.Flush()
	if err = writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), rows.Err()
}

func (h *Handler) exportBatch(w http.ResponseWriter, r *http.Request) {
	user, ok := h.auth.Authorize(w, r, false, auth.Superadmin, auth.Admin, auth.Researcher, auth.Reviewer)
	if !ok || !validID(r.PathValue("id")) {
		if ok {
			writeError(w, 404, "Project not found")
		}
		return
	}
	allowed, code, err := h.canAccessProject(r.Context(), user, r.PathValue("id"))
	if err != nil || !allowed {
		h.accessError(w, err)
		return
	}
	type member struct {
		name     string
		contents []byte
	}
	members := []member{}
	for _, item := range []struct {
		name  string
		build csvBuilder
	}{{"summary.csv", h.summaryCSV}, {"events.csv", h.eventsCSV}, {"gold.csv", h.goldCSV}} {
		contents, e := item.build(r.Context(), r.PathValue("id"))
		if e != nil {
			h.internal(w, e)
			return
		}
		members = append(members, member{item.name, contents})
	}
	rows, err := h.db.Query(r.Context(), `SELECT r.id,a.metadata_path,a.report_path FROM recordings r JOIN LATERAL (SELECT metadata_path,report_path FROM recording_analyses WHERE recording_id=r.id ORDER BY created_at DESC,id DESC LIMIT 1) a ON true WHERE r.project_id=$1 ORDER BY r.id`, r.PathValue("id"))
	if err != nil {
		h.internal(w, err)
		return
	}
	var totalSize int64
	for _, item := range members {
		totalSize += int64(len(item.contents))
	}
	for rows.Next() {
		var recordingID, metadataPath, reportPath string
		if err = rows.Scan(&recordingID, &metadataPath, &reportPath); err != nil {
			rows.Close()
			h.internal(w, err)
			return
		}
		for _, source := range []struct{ kind, path string }{{"metadata", metadataPath}, {"reports", reportPath}} {
			path, e := safeDataPath(h.dataDir, source.path)
			if e != nil {
				rows.Close()
				h.internal(w, e)
				return
			}
			contents, e := os.ReadFile(path)
			if e != nil {
				rows.Close()
				h.internal(w, e)
				return
			}
			totalSize += int64(len(contents))
			if totalSize > 512*1024*1024 {
				rows.Close()
				writeError(w, http.StatusRequestEntityTooLarge, "Export exceeds the 512 MB limit")
				return
			}
			members = append(members, member{fmt.Sprintf("%s/%s/%s", source.kind, recordingID, filepath.Base(path)), contents})
		}
	}
	rows.Close()
	var manifest strings.Builder
	manifest.WriteString("sha256  path\n")
	for _, item := range members {
		sum := sha256.Sum256(item.contents)
		fmt.Fprintf(&manifest, "%s  %s\n", hex.EncodeToString(sum[:]), item.name)
	}
	members = append(members, member{"manifest.sha256", []byte(manifest.String())})
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-analysis.zip"`, code))
	w.Header().Set("Cache-Control", "private, no-store")
	archiveHash := sha256.New()
	archive := zip.NewWriter(io.MultiWriter(w, archiveHash))
	for _, item := range members {
		header := &zip.FileHeader{Name: item.name, Method: zip.Deflate}
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		entry, e := archive.CreateHeader(header)
		if e != nil {
			h.logger.Error("create export entry", slog.Any("error", e))
			return
		}
		if _, e = io.Copy(entry, bytes.NewReader(item.contents)); e != nil {
			h.logger.Error("write export entry", slog.Any("error", e))
			return
		}
	}
	if err := archive.Close(); err != nil {
		h.logger.Error("close export archive", slog.Any("error", err))
		return
	}
	h.auditExport(r.Context(), r.PathValue("id"), user.ID, "batch_zip", hex.EncodeToString(archiveHash.Sum(nil)))
}

func (h *Handler) auditExport(ctx context.Context, projectID, userID, exportType, checksum string) {
	_, err := h.db.Exec(ctx, `INSERT INTO export_audits(project_id,requested_by,export_type,algorithm_version,status,output_sha256,completed_at) SELECT $1,$2,$3,'signal-v3-p' || version,'completed',$4,NOW() FROM analysis_profiles WHERE project_id=$1`, projectID, userID, exportType, checksum)
	if err != nil {
		h.logger.Error("record export audit", slog.Any("error", err), slog.String("project_id", projectID))
	}
}

func safeDataPath(dataDir, relative string) (string, error) {
	base, err := filepath.Abs(dataDir)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(base, filepath.FromSlash(relative)))
	if err != nil {
		return "", err
	}
	if target == base || !strings.HasPrefix(target, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("artifact path escapes data directory")
	}
	return target, nil
}

func (h *Handler) canAccessProject(ctx context.Context, user auth.User, projectID string) (bool, string, error) {
	var allowed bool
	var code string
	err := h.db.QueryRow(ctx, `SELECT p.code,($2='superadmin' OR ($2='admin' AND (p.created_by=$3 OR (u.institution_id IS NOT NULL AND p.institution_id=u.institution_id))) OR pm.user_id=$3) FROM projects p JOIN users u ON u.id=$3 LEFT JOIN project_members pm ON pm.project_id=p.id AND pm.user_id=$3 WHERE p.id=$1`, projectID, user.Role, user.ID).Scan(&code, &allowed)
	return allowed, code, err
}
