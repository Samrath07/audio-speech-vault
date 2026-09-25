package workspace

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shivam3746/audio-speech-vault/internal/auth"
)

var (
	ErrInvalid  = errors.New("invalid workspace data")
	ErrDenied   = errors.New("workspace access denied")
	ErrNotFound = errors.New("workspace item not found")
	ErrConflict = errors.New("workspace item changed; refresh and try again")
)

var projectCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,31}$`)

type Project struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

type Tier struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	DisplayOrder int      `json:"displayOrder"`
	Color        string   `json:"color"`
	Required     bool     `json:"required"`
	Values       []string `json:"values"`
}

type Recording struct {
	ID               string  `json:"id"`
	ProjectID        string  `json:"projectId"`
	Filename         string  `json:"filename"`
	MediaType        string  `json:"mediaType"`
	SizeBytes        int64   `json:"sizeBytes"`
	DurationMS       int64   `json:"durationMs"`
	Status           string  `json:"status"`
	TaskStatus       string  `json:"taskStatus"`
	ResearcherID     *string `json:"researcherId,omitempty"`
	ReviewerID       *string `json:"reviewerId,omitempty"`
	AnalysisStatus   string  `json:"analysisStatus"`
	AnalysisVersion  string  `json:"analysisVersion,omitempty"`
	AnalysisAttempts int     `json:"analysisAttempts"`
	AnalysisError    *string `json:"analysisError,omitempty"`
	StorageKey       string  `json:"-"`
}

type Person struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	DisplayName string     `json:"displayName"`
	Role        auth.Role  `json:"role"`
	ProjectRole *auth.Role `json:"projectRole,omitempty"`
}

type Annotation struct {
	ID          string `json:"id"`
	RecordingID string `json:"recordingId"`
	TierID      string `json:"tierId"`
	StartMS     int64  `json:"startMs"`
	EndMS       int64  `json:"endMs"`
	Value       string `json:"value"`
	Version     int    `json:"version"`
}

type Workspace struct {
	Recording   Recording    `json:"recording"`
	Tiers       []Tier       `json:"tiers"`
	Annotations []Annotation `json:"annotations"`
	CanEdit     bool         `json:"canEdit"`
	CanReview   bool         `json:"canReview"`
}

type Store struct{ db *pgxpool.Pool }

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

func cleanText(value string, max int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > max || strings.ContainsAny(value, "\x00\r\n") {
		return "", ErrInvalid
	}
	return value, nil
}

func validTierType(value string) bool {
	return value == "text" || value == "controlled_vocabulary" || value == "tag" || value == "comment"
}

func canManage(role auth.Role) bool { return role == auth.Superadmin || role == auth.Admin }

func (s *Store) ListProjects(ctx context.Context, user auth.User) ([]Project, error) {
	rows, err := s.db.Query(ctx, `SELECT DISTINCT p.id,p.code,p.name,p.description,p.status
		FROM projects p LEFT JOIN project_members pm ON pm.project_id=p.id
		WHERE $1='superadmin' OR ($1='admin' AND (p.created_by=$2 OR EXISTS(
			SELECT 1 FROM users u WHERE u.id=$2 AND u.institution_id IS NOT NULL AND p.institution_id=u.institution_id))) OR pm.user_id=$2
		ORDER BY p.name`, user.Role, user.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := []Project{}
	for rows.Next() {
		var project Project
		if err := rows.Scan(&project.ID, &project.Code, &project.Name, &project.Description, &project.Status); err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

func (s *Store) CreateProject(ctx context.Context, user auth.User, code, name, description string) (Project, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	var err error
	if !projectCodePattern.MatchString(code) {
		return Project{}, fmt.Errorf("%w: code must use 2-32 lowercase letters, numbers, or hyphens", ErrInvalid)
	}
	if name, err = cleanText(name, 120); err != nil {
		return Project{}, fmt.Errorf("%w: invalid project name", ErrInvalid)
	}
	description = strings.TrimSpace(description)
	if len(description) > 2000 {
		return Project{}, fmt.Errorf("%w: description is too long", ErrInvalid)
	}
	var project Project
	err = s.db.QueryRow(ctx, `INSERT INTO projects(code,name,description,created_by,institution_id)
		VALUES($1,$2,$3,$4,(SELECT institution_id FROM users WHERE id=$4))
		RETURNING id,code,name,description,status`, code, name, description, user.ID).Scan(&project.ID, &project.Code, &project.Name, &project.Description, &project.Status)
	if err != nil {
		return Project{}, err
	}
	return project, nil
}

func (s *Store) hasProjectAccess(ctx context.Context, user auth.User, projectID string, manage bool) (bool, error) {
	if user.Role == auth.Superadmin {
		return true, nil
	}
	var allowed bool
	if user.Role == auth.Admin {
		err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM projects p JOIN users u ON u.id=$2
			WHERE p.id=$1 AND (p.created_by=$2 OR (u.institution_id IS NOT NULL AND p.institution_id=u.institution_id)))`, projectID, user.ID).Scan(&allowed)
		return allowed, err
	}
	if manage {
		return false, nil
	}
	err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM project_members WHERE project_id=$1 AND user_id=$2)`, projectID, user.ID).Scan(&allowed)
	return allowed, err
}

func (s *Store) AddMember(ctx context.Context, actor auth.User, projectID, userID string, responsibility auth.Role) error {
	allowed, err := s.hasProjectAccess(ctx, actor, projectID, true)
	if err != nil || !allowed {
		if err != nil {
			return err
		}
		return ErrDenied
	}
	if responsibility != auth.Researcher && responsibility != auth.Reviewer {
		return fmt.Errorf("%w: invalid project responsibility", ErrInvalid)
	}
	command, err := s.db.Exec(ctx, `INSERT INTO project_members(project_id,user_id,role)
		SELECT $1,id,$3 FROM users WHERE id=$2 AND is_active AND role=$3
		ON CONFLICT(project_id,user_id) DO UPDATE SET role=EXCLUDED.role`, projectID, userID, responsibility)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return fmt.Errorf("%w: user must be active and have the matching global role", ErrInvalid)
	}
	return nil
}

func (s *Store) ListPeople(ctx context.Context, actor auth.User, projectID string) ([]Person, error) {
	allowed, err := s.hasProjectAccess(ctx, actor, projectID, true)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrDenied
	}
	rows, err := s.db.Query(ctx, `SELECT u.id,u.email,u.display_name,u.role,pm.role
		FROM users u JOIN projects p ON p.id=$1
		LEFT JOIN project_members pm ON pm.project_id=p.id AND pm.user_id=u.id
		WHERE u.is_active AND u.role IN ('researcher','reviewer') AND
		($2='superadmin' OR (u.institution_id IS NOT NULL AND u.institution_id=p.institution_id))
		ORDER BY u.role,u.display_name,u.email`, projectID, actor.Role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	people := []Person{}
	for rows.Next() {
		var person Person
		if err := rows.Scan(&person.ID, &person.Email, &person.DisplayName, &person.Role, &person.ProjectRole); err != nil {
			return nil, err
		}
		people = append(people, person)
	}
	return people, rows.Err()
}

func (s *Store) ListTiers(ctx context.Context, user auth.User, projectID string) ([]Tier, error) {
	allowed, err := s.hasProjectAccess(ctx, user, projectID, false)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrDenied
	}
	rows, err := s.db.Query(ctx, `SELECT t.id,t.name,t.tier_type,t.display_order,t.color,t.is_required,
		COALESCE(array_agg(v.value ORDER BY v.display_order,v.value) FILTER (WHERE v.id IS NOT NULL AND v.is_active),'{}')
		FROM tier_definitions t LEFT JOIN tier_values v ON v.tier_id=t.id
		WHERE t.project_id=$1 AND t.is_active GROUP BY t.id ORDER BY t.display_order,t.id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tiers := []Tier{}
	for rows.Next() {
		var tier Tier
		if err := rows.Scan(&tier.ID, &tier.Name, &tier.Type, &tier.DisplayOrder, &tier.Color, &tier.Required, &tier.Values); err != nil {
			return nil, err
		}
		tiers = append(tiers, tier)
	}
	return tiers, rows.Err()
}

func (s *Store) CreateTier(ctx context.Context, user auth.User, projectID string, tier Tier) (Tier, error) {
	allowed, err := s.hasProjectAccess(ctx, user, projectID, true)
	if err != nil {
		return Tier{}, err
	}
	if !allowed {
		return Tier{}, ErrDenied
	}
	if tier.Name, err = cleanText(tier.Name, 80); err != nil || !validTierType(tier.Type) || tier.DisplayOrder < 0 || !regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`).MatchString(tier.Color) {
		return Tier{}, fmt.Errorf("%w: invalid tier definition", ErrInvalid)
	}
	if tier.Type != "controlled_vocabulary" && len(tier.Values) > 0 {
		return Tier{}, fmt.Errorf("%w: only controlled-vocabulary tiers accept values", ErrInvalid)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Tier{}, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `INSERT INTO tier_definitions(project_id,name,tier_type,display_order,color,is_required) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, projectID, tier.Name, tier.Type, tier.DisplayOrder, tier.Color, tier.Required).Scan(&tier.ID)
	if err != nil {
		return Tier{}, err
	}
	seen := map[string]bool{}
	for i, value := range tier.Values {
		value, err = cleanText(value, 120)
		if err != nil || seen[value] {
			return Tier{}, fmt.Errorf("%w: invalid or duplicate tier value", ErrInvalid)
		}
		seen[value] = true
		if _, err = tx.Exec(ctx, `INSERT INTO tier_values(tier_id,value,label,display_order) VALUES($1,$2,$2,$3)`, tier.ID, value, i); err != nil {
			return Tier{}, err
		}
		tier.Values[i] = value
	}
	return tier, tx.Commit(ctx)
}

func (s *Store) ListRecordings(ctx context.Context, user auth.User, projectID string) ([]Recording, error) {
	allowed, err := s.hasProjectAccess(ctx, user, projectID, false)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrDenied
	}
	rows, err := s.db.Query(ctx, `SELECT r.id,r.project_id,r.original_filename,r.media_type,r.size_bytes,r.duration_ms,r.status,t.status,t.assigned_researcher_id,t.assigned_reviewer_id,COALESCE(j.status,'not_queued'),COALESCE(j.algorithm_version,''),COALESCE(j.attempts,0),j.error_code
		FROM recordings r JOIN annotation_tasks t ON t.recording_id=r.id LEFT JOIN LATERAL (SELECT status,algorithm_version,attempts,error_code FROM analysis_jobs WHERE recording_id=r.id ORDER BY created_at DESC,id DESC LIMIT 1) j ON true WHERE r.project_id=$1
		AND ($2 IN ('superadmin','admin') OR t.assigned_researcher_id=$3 OR t.assigned_reviewer_id=$3) ORDER BY r.created_at DESC`, projectID, user.Role, user.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Recording{}
	for rows.Next() {
		var item Recording
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.Filename, &item.MediaType, &item.SizeBytes, &item.DurationMS, &item.Status, &item.TaskStatus, &item.ResearcherID, &item.ReviewerID, &item.AnalysisStatus, &item.AnalysisVersion, &item.AnalysisAttempts, &item.AnalysisError); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateRecording(ctx context.Context, user auth.User, projectID string, item Recording) (Recording, error) {
	allowed, err := s.hasProjectAccess(ctx, user, projectID, true)
	if err != nil {
		return Recording{}, err
	}
	if !allowed {
		return Recording{}, ErrDenied
	}
	if item.Filename, err = cleanText(filepath.Base(item.Filename), 255); err != nil || item.SizeBytes <= 0 || item.SizeBytes > 524288000 || item.DurationMS <= 0 || item.DurationMS > 86400000 {
		return Recording{}, fmt.Errorf("%w: invalid recording metadata", ErrInvalid)
	}
	media := map[string]bool{"audio/mpeg": true, "audio/wav": true, "audio/x-wav": true, "audio/flac": true, "audio/ogg": true, "audio/webm": true}
	if !media[item.MediaType] {
		return Recording{}, fmt.Errorf("%w: unsupported audio type", ErrInvalid)
	}
	if item.ResearcherID != nil && item.ReviewerID != nil && *item.ResearcherID == *item.ReviewerID {
		return Recording{}, fmt.Errorf("%w: researcher and reviewer must differ", ErrInvalid)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Recording{}, err
	}
	defer tx.Rollback(ctx)
	for id, role := range map[*string]auth.Role{item.ResearcherID: auth.Researcher, item.ReviewerID: auth.Reviewer} {
		if id == nil {
			continue
		}
		var ok bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM project_members pm JOIN users u ON u.id=pm.user_id WHERE pm.project_id=$1 AND pm.user_id=$2 AND pm.role=$3 AND u.role=$3 AND u.is_active)`, projectID, *id, role).Scan(&ok); err != nil {
			return Recording{}, err
		}
		if !ok {
			return Recording{}, fmt.Errorf("%w: assignee is not an eligible project member", ErrInvalid)
		}
	}
	storageKey := item.StorageKey
	if storageKey == "" {
		storageKey = uuid.NewString() + strings.ToLower(filepath.Ext(item.Filename))
	}
	item.ProjectID = projectID
	item.Status = "ready"
	err = tx.QueryRow(ctx, `INSERT INTO recordings(project_id,original_filename,storage_key,media_type,size_bytes,duration_ms,uploaded_by) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, projectID, item.Filename, storageKey, item.MediaType, item.SizeBytes, item.DurationMS, user.ID).Scan(&item.ID)
	if err != nil {
		return Recording{}, err
	}
	item.TaskStatus = "unassigned"
	if item.ResearcherID != nil {
		item.TaskStatus = "in_progress"
	}
	_, err = tx.Exec(ctx, `INSERT INTO annotation_tasks(recording_id,assigned_researcher_id,assigned_reviewer_id,status) VALUES($1,$2,$3,$4)`, item.ID, item.ResearcherID, item.ReviewerID, item.TaskStatus)
	if err != nil {
		return Recording{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO analysis_profiles(project_id,updated_by) VALUES($1,$2) ON CONFLICT(project_id) DO NOTHING`, projectID, user.ID)
	if err != nil {
		return Recording{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO analysis_jobs(recording_id,algorithm_version,configuration)
		SELECT $1,'signal-v3-p' || version,jsonb_build_object('silenceDbfs',silence_dbfs,'minimumPauseMs',minimum_pause_ms,'longPauseMs',long_pause_ms,'repetitionSimilarity',repetition_similarity,'speakerSeparation',speaker_separation)
		FROM analysis_profiles WHERE project_id=$2`, item.ID, projectID)
	if err != nil {
		return Recording{}, err
	}
	return item, tx.Commit(ctx)
}

func (s *Store) MediaPath(ctx context.Context, user auth.User, recordingID, audioDirectory string) (string, string, error) {
	space, err := s.GetWorkspace(ctx, user, recordingID)
	if err != nil {
		return "", "", err
	}
	var storageKey, mediaType string
	err = s.db.QueryRow(ctx, `SELECT storage_key,media_type FROM recordings WHERE id=$1 AND status='ready'`, space.Recording.ID).Scan(&storageKey, &mediaType)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	base, err := filepath.Abs(audioDirectory)
	if err != nil {
		return "", "", err
	}
	path, err := filepath.Abs(filepath.Join(base, storageKey))
	if err != nil {
		return "", "", err
	}
	if filepath.Dir(path) != base {
		return "", "", ErrDenied
	}
	return path, mediaType, nil
}

func (s *Store) GetWorkspace(ctx context.Context, user auth.User, recordingID string) (Workspace, error) {
	var result Workspace
	err := s.db.QueryRow(ctx, `SELECT r.id,r.project_id,r.original_filename,r.media_type,r.size_bytes,r.duration_ms,r.status,t.status,t.assigned_researcher_id,t.assigned_reviewer_id
		FROM recordings r JOIN annotation_tasks t ON t.recording_id=r.id WHERE r.id=$1`, recordingID).Scan(&result.Recording.ID, &result.Recording.ProjectID, &result.Recording.Filename, &result.Recording.MediaType, &result.Recording.SizeBytes, &result.Recording.DurationMS, &result.Recording.Status, &result.Recording.TaskStatus, &result.Recording.ResearcherID, &result.Recording.ReviewerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	allowed, err := s.hasProjectAccess(ctx, user, result.Recording.ProjectID, false)
	if err != nil {
		return result, err
	}
	if !allowed {
		return result, ErrDenied
	}
	if !canManage(user.Role) {
		assigned := (result.Recording.ResearcherID != nil && *result.Recording.ResearcherID == user.ID) || (result.Recording.ReviewerID != nil && *result.Recording.ReviewerID == user.ID)
		if !assigned {
			return result, ErrDenied
		}
	}
	result.CanEdit = canManage(user.Role) || (user.Role == auth.Researcher && result.Recording.ResearcherID != nil && *result.Recording.ResearcherID == user.ID && (result.Recording.TaskStatus == "in_progress" || result.Recording.TaskStatus == "changes_requested"))
	result.CanReview = canManage(user.Role) || (user.Role == auth.Reviewer && result.Recording.ReviewerID != nil && *result.Recording.ReviewerID == user.ID && result.Recording.TaskStatus == "submitted")
	result.Tiers, err = s.ListTiers(ctx, user, result.Recording.ProjectID)
	if err != nil {
		return result, err
	}
	rows, err := s.db.Query(ctx, `SELECT id,recording_id,tier_id,start_ms,end_ms,value,version FROM annotations WHERE recording_id=$1 AND deleted_at IS NULL ORDER BY start_ms,end_ms,id`, recordingID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	result.Annotations = []Annotation{}
	for rows.Next() {
		var a Annotation
		if err := rows.Scan(&a.ID, &a.RecordingID, &a.TierID, &a.StartMS, &a.EndMS, &a.Value, &a.Version); err != nil {
			return result, err
		}
		result.Annotations = append(result.Annotations, a)
	}
	return result, rows.Err()
}

func (s *Store) SaveAnnotation(ctx context.Context, user auth.User, item Annotation, create bool) (Annotation, error) {
	space, err := s.GetWorkspace(ctx, user, item.RecordingID)
	if err != nil {
		return item, err
	}
	if !space.CanEdit {
		return item, ErrDenied
	}
	item.Value = strings.TrimSpace(item.Value)
	if item.StartMS < 0 || item.EndMS <= item.StartMS || item.EndMS > space.Recording.DurationMS || item.Value == "" || len(item.Value) > 4000 {
		return item, fmt.Errorf("%w: invalid annotation range or value", ErrInvalid)
	}
	var tier *Tier
	for i := range space.Tiers {
		if space.Tiers[i].ID == item.TierID {
			tier = &space.Tiers[i]
			break
		}
	}
	if tier == nil {
		return item, fmt.Errorf("%w: tier does not belong to this project", ErrInvalid)
	}
	if tier.Type == "controlled_vocabulary" {
		valid := false
		for _, value := range tier.Values {
			if value == item.Value {
				valid = true
			}
		}
		if !valid {
			return item, fmt.Errorf("%w: value is not allowed for this tier", ErrInvalid)
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return item, err
	}
	defer tx.Rollback(ctx)
	if create {
		item.Version = 1
		err = tx.QueryRow(ctx, `INSERT INTO annotations(recording_id,tier_id,start_ms,end_ms,value,created_by,updated_by) VALUES($1,$2,$3,$4,$5,$6,$6) RETURNING id`, item.RecordingID, item.TierID, item.StartMS, item.EndMS, item.Value, user.ID).Scan(&item.ID)
	} else {
		command, e := tx.Exec(ctx, `UPDATE annotations SET tier_id=$1,start_ms=$2,end_ms=$3,value=$4,updated_by=$5,version=version+1,updated_at=NOW() WHERE id=$6 AND recording_id=$7 AND version=$8 AND deleted_at IS NULL`, item.TierID, item.StartMS, item.EndMS, item.Value, user.ID, item.ID, item.RecordingID, item.Version)
		err = e
		if err == nil && command.RowsAffected() == 0 {
			return item, ErrConflict
		}
		item.Version++
	}
	if err != nil {
		return item, err
	}
	change := "updated"
	if create {
		change = "created"
	}
	_, err = tx.Exec(ctx, `INSERT INTO annotation_revisions(annotation_id,revision_number,start_ms,end_ms,value,changed_by,change_type) VALUES($1,$2,$3,$4,$5,$6,$7)`, item.ID, item.Version, item.StartMS, item.EndMS, item.Value, user.ID, change)
	if err != nil {
		return item, err
	}
	return item, tx.Commit(ctx)
}

func (s *Store) SetAnnotationDeleted(ctx context.Context, user auth.User, recordingID, annotationID string, version int, deleted bool) (Annotation, error) {
	space, err := s.GetWorkspace(ctx, user, recordingID)
	if err != nil {
		return Annotation{}, err
	}
	if !space.CanEdit {
		return Annotation{}, ErrDenied
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Annotation{}, err
	}
	defer tx.Rollback(ctx)
	item := Annotation{ID: annotationID, RecordingID: recordingID}
	deletedAtClause := "NOW()"
	currentState := "deleted_at IS NULL"
	change := "deleted"
	if !deleted {
		deletedAtClause = "NULL"
		currentState = "deleted_at IS NOT NULL"
		change = "updated"
	}
	query := fmt.Sprintf(`UPDATE annotations SET deleted_at=%s,updated_by=$1,version=version+1,updated_at=NOW() WHERE id=$2 AND recording_id=$3 AND version=$4 AND %s RETURNING tier_id,start_ms,end_ms,value,version`, deletedAtClause, currentState)
	err = tx.QueryRow(ctx, query, user.ID, annotationID, recordingID, version).Scan(&item.TierID, &item.StartMS, &item.EndMS, &item.Value, &item.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, ErrConflict
	}
	if err != nil {
		return item, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO annotation_revisions(annotation_id,revision_number,start_ms,end_ms,value,changed_by,change_type) VALUES($1,$2,$3,$4,$5,$6,$7)`, item.ID, item.Version, item.StartMS, item.EndMS, item.Value, user.ID, change)
	if err != nil {
		return item, err
	}
	return item, tx.Commit(ctx)
}

func (s *Store) Submit(ctx context.Context, user auth.User, recordingID string) error {
	space, err := s.GetWorkspace(ctx, user, recordingID)
	if err != nil {
		return err
	}
	if !space.CanEdit {
		return ErrDenied
	}
	for _, tier := range space.Tiers {
		if !tier.Required {
			continue
		}
		found := false
		for _, a := range space.Annotations {
			if a.TierID == tier.ID {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%w: required tier %q has no annotations", ErrInvalid, tier.Name)
		}
	}
	allowedStatuses := []string{"in_progress", "changes_requested"}
	if canManage(user.Role) {
		allowedStatuses = append(allowedStatuses, "unassigned")
	}
	command, err := s.db.Exec(ctx, `UPDATE annotation_tasks SET status='submitted',submitted_at=NOW(),updated_at=NOW() WHERE recording_id=$1 AND status=ANY($2)`, recordingID, allowedStatuses)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (s *Store) Review(ctx context.Context, user auth.User, recordingID, decision, comment string) error {
	space, err := s.GetWorkspace(ctx, user, recordingID)
	if err != nil {
		return err
	}
	if !space.CanReview {
		return ErrDenied
	}
	if decision != "approved" && decision != "changes_requested" {
		return fmt.Errorf("%w: invalid review decision", ErrInvalid)
	}
	comment = strings.TrimSpace(comment)
	if decision == "changes_requested" && comment == "" {
		return fmt.Errorf("%w: a comment is required when requesting changes", ErrInvalid)
	}
	if len(comment) > 4000 {
		return ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	command, err := tx.Exec(ctx, `UPDATE annotation_tasks SET status=$1,reviewed_at=NOW(),updated_at=NOW() WHERE recording_id=$2 AND status='submitted'`, decision, recordingID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrConflict
	}
	if comment != "" {
		_, err = tx.Exec(ctx, `INSERT INTO review_comments(task_id,author_id,body) SELECT id,$2,$3 FROM annotation_tasks WHERE recording_id=$1`, recordingID, user.ID, comment)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
