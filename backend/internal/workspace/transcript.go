package workspace

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/shivam3746/audio-speech-vault/internal/auth"
)

type TranscriptSegment struct {
	ID          string `json:"id"`
	RecordingID string `json:"recordingId"`
	StartMS     int64  `json:"startMs"`
	EndMS       int64  `json:"endMs"`
	SpeakerRole string `json:"speakerRole"`
	Text        string `json:"text"`
	Source      string `json:"source"`
	Version     int    `json:"version"`
}

type TranscriptFinding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type Transcript struct {
	ID                  string              `json:"id"`
	RecordingID         string              `json:"recordingId"`
	ConventionVersion   int                 `json:"conventionVersion"`
	TargetSpeakerSource *string             `json:"targetSpeakerSource,omitempty"`
	Segments            []TranscriptSegment `json:"segments"`
	Findings            []TranscriptFinding `json:"findings"`
	CanEdit             bool                `json:"canEdit"`
}

func validSpeakerRole(value string) bool {
	switch value {
	case "target", "interviewer", "other", "unknown", "overlap":
		return true
	default:
		return false
	}
}

func (s *Store) GetTranscript(ctx context.Context, user auth.User, recordingID string) (Transcript, error) {
	space, err := s.GetWorkspace(ctx, user, recordingID)
	if err != nil {
		return Transcript{}, err
	}
	result := Transcript{RecordingID: recordingID, Segments: []TranscriptSegment{}, CanEdit: space.CanEdit}
	err = s.db.QueryRow(ctx, `INSERT INTO recording_transcripts(recording_id) VALUES($1)
		ON CONFLICT(recording_id) DO UPDATE SET recording_id=EXCLUDED.recording_id
		RETURNING id,convention_version,target_speaker_source`, recordingID).
		Scan(&result.ID, &result.ConventionVersion, &result.TargetSpeakerSource)
	if err != nil {
		return result, err
	}
	rows, err := s.db.Query(ctx, `SELECT id,recording_id,start_ms,end_ms,speaker_role,text,source,version
		FROM transcript_segments WHERE recording_id=$1 AND deleted_at IS NULL ORDER BY start_ms,end_ms,id`, recordingID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item TranscriptSegment
		if err := rows.Scan(&item.ID, &item.RecordingID, &item.StartMS, &item.EndMS, &item.SpeakerRole, &item.Text, &item.Source, &item.Version); err != nil {
			return result, err
		}
		result.Segments = append(result.Segments, item)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	result.Findings = transcriptFindings(result)
	return result, nil
}

func transcriptFindings(transcript Transcript) []TranscriptFinding {
	items := []TranscriptFinding{}
	if transcript.TargetSpeakerSource == nil {
		items = append(items, TranscriptFinding{Code: "target_speaker_unconfirmed", Severity: "error", Message: "Confirm how the target speaker was identified."})
	}
	if len(transcript.Segments) == 0 {
		items = append(items, TranscriptFinding{Code: "transcript_empty", Severity: "error", Message: "Add at least one transcript segment."})
	}
	target := false
	for _, segment := range transcript.Segments {
		if segment.SpeakerRole == "target" {
			target = true
		}
		if segment.SpeakerRole == "unknown" {
			items = append(items, TranscriptFinding{Code: "unknown_speaker", Severity: "warning", Message: "One or more segments still have an unknown speaker."})
			break
		}
	}
	if len(transcript.Segments) > 0 && !target {
		items = append(items, TranscriptFinding{Code: "target_speech_missing", Severity: "error", Message: "Mark at least one segment as target-speaker speech."})
	}
	return items
}

func (s *Store) SetTargetSpeaker(ctx context.Context, user auth.User, recordingID, source string) (Transcript, error) {
	space, err := s.GetWorkspace(ctx, user, recordingID)
	if err != nil {
		return Transcript{}, err
	}
	if !space.CanEdit {
		return Transcript{}, ErrDenied
	}
	if source != "speaker_a" && source != "speaker_b" && source != "manual" {
		return Transcript{}, fmt.Errorf("%w: invalid target-speaker source", ErrInvalid)
	}
	_, err = s.db.Exec(ctx, `INSERT INTO recording_transcripts(recording_id,target_speaker_source,target_speaker_confirmed_by,target_speaker_confirmed_at)
		VALUES($1,$2,$3,NOW()) ON CONFLICT(recording_id) DO UPDATE SET target_speaker_source=$2,target_speaker_confirmed_by=$3,target_speaker_confirmed_at=NOW(),updated_at=NOW()`, recordingID, source, user.ID)
	if err != nil {
		return Transcript{}, err
	}
	return s.GetTranscript(ctx, user, recordingID)
}

func (s *Store) SaveTranscriptSegment(ctx context.Context, user auth.User, item TranscriptSegment, create bool) (TranscriptSegment, error) {
	transcript, err := s.GetTranscript(ctx, user, item.RecordingID)
	if err != nil {
		return item, err
	}
	if !transcript.CanEdit {
		return item, ErrDenied
	}
	item.Text = strings.TrimSpace(item.Text)
	var duration int64
	if err := s.db.QueryRow(ctx, `SELECT duration_ms FROM recordings WHERE id=$1`, item.RecordingID).Scan(&duration); err != nil {
		return item, err
	}
	if item.StartMS < 0 || item.EndMS <= item.StartMS || item.EndMS > duration || len(item.Text) == 0 || len(item.Text) > 8000 || !validSpeakerRole(item.SpeakerRole) {
		return item, fmt.Errorf("%w: invalid transcript segment", ErrInvalid)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return item, err
	}
	defer tx.Rollback(ctx)
	if create {
		item.Version = 1
		item.Source = "human"
		err = tx.QueryRow(ctx, `INSERT INTO transcript_segments(transcript_id,recording_id,start_ms,end_ms,speaker_role,text,created_by,updated_by)
			VALUES($1,$2,$3,$4,$5,$6,$7,$7) RETURNING id,source`, transcript.ID, item.RecordingID, item.StartMS, item.EndMS, item.SpeakerRole, item.Text, user.ID).Scan(&item.ID, &item.Source)
	} else {
		err = tx.QueryRow(ctx, `UPDATE transcript_segments SET start_ms=$1,end_ms=$2,speaker_role=$3,text=$4,updated_by=$5,version=version+1,updated_at=NOW()
			WHERE id=$6 AND recording_id=$7 AND version=$8 AND deleted_at IS NULL RETURNING source,version`, item.StartMS, item.EndMS, item.SpeakerRole, item.Text, user.ID, item.ID, item.RecordingID, item.Version).Scan(&item.Source, &item.Version)
		if errors.Is(err, pgx.ErrNoRows) {
			return item, ErrConflict
		}
	}
	if err != nil {
		return item, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO transcript_segment_revisions(segment_id,revision_number,start_ms,end_ms,speaker_role,text,changed_by,change_type)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, item.ID, item.Version, item.StartMS, item.EndMS, item.SpeakerRole, item.Text, user.ID, map[bool]string{true: "created", false: "updated"}[create])
	if err != nil {
		return item, err
	}
	return item, tx.Commit(ctx)
}

func (s *Store) DeleteTranscriptSegment(ctx context.Context, user auth.User, recordingID, segmentID string, version int) error {
	transcript, err := s.GetTranscript(ctx, user, recordingID)
	if err != nil {
		return err
	}
	if !transcript.CanEdit {
		return ErrDenied
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var startMS, endMS int64
	var role, text string
	err = tx.QueryRow(ctx, `UPDATE transcript_segments SET deleted_at=NOW(),updated_by=$1,version=version+1,updated_at=NOW()
		WHERE id=$2 AND recording_id=$3 AND version=$4 AND deleted_at IS NULL RETURNING start_ms,end_ms,speaker_role,text,version`, user.ID, segmentID, recordingID, version).
		Scan(&startMS, &endMS, &role, &text, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO transcript_segment_revisions(segment_id,revision_number,start_ms,end_ms,speaker_role,text,changed_by,change_type)
		VALUES($1,$2,$3,$4,$5,$6,$7,'deleted')`, segmentID, version, startMS, endMS, role, text, user.ID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
