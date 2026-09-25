package workspace

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shivam3746/audio-speech-vault/internal/auth"
)

func TestValidationGuardrails(t *testing.T) {
	if _, err := cleanText("\n", 10); !errors.Is(err, ErrInvalid) {
		t.Fatal("blank text should fail")
	}
	if _, err := cleanText("line\nbreak", 20); !errors.Is(err, ErrInvalid) {
		t.Fatal("line breaks should fail")
	}
	if projectCodePattern.MatchString("UPPER CASE") {
		t.Fatal("unsafe project code should fail")
	}
	if !validTierType("controlled_vocabulary") || validTierType("sql") {
		t.Fatal("tier type allowlist failed")
	}
}

func TestAudioSignatures(t *testing.T) {
	tests := []struct {
		name      string
		extension string
		contents  []byte
		valid     bool
	}{
		{"id3 mp3", ".mp3", []byte("ID3\x04\x00"), true},
		{"frame mp3", ".mp3", []byte{0xff, 0xfb, 0x90, 0x64}, true},
		{"wav", ".wav", []byte("RIFF1234WAVE"), true},
		{"disguised executable", ".mp3", []byte("MZ executable"), false},
		{"wrong extension", ".wav", []byte("ID3\x04\x00"), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validAudioContent(test.extension, test.contents); got != test.valid {
				t.Fatalf("got %v, want %v", got, test.valid)
			}
		})
	}
}

func TestWorkspaceLifecycle(t *testing.T) {
	databaseURL := os.Getenv("TEST_WORKSPACE_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_WORKSPACE_DATABASE_URL not set")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil || !strings.HasSuffix(config.ConnConfig.Database, "_workspace_test") {
		t.Fatal("integration test requires a dedicated database ending in _workspace_test")
	}
	db, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	_, err = db.Exec(ctx, `TRUNCATE review_comments,annotation_revisions,annotations,annotation_tasks,tier_values,tier_definitions,recordings,audit_events,project_members,projects,password_setup_tokens,access_requests,sessions,users,institutions CASCADE`)
	if err != nil {
		t.Fatal(err)
	}
	inst1, inst2 := uuid.NewString(), uuid.NewString()
	admin1, admin2, researcher, reviewer := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	_, err = db.Exec(ctx, `INSERT INTO institutions(id,name,email_domain) VALUES($1,'One','one.test'),($2,'Two','two.test')`, inst1, inst2)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(ctx, `INSERT INTO users(id,email,display_name,password_hash,role,institution_id) VALUES
		($3,'admin@one.test','Admin One','x','admin',$1),($4,'admin@two.test','Admin Two','x','admin',$2),
		($5,'researcher@one.test','Researcher','x','researcher',$1),($6,'reviewer@one.test','Reviewer','x','reviewer',$1)`, inst1, inst2, admin1, admin2, researcher, reviewer)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	a1 := auth.User{ID: admin1, Role: auth.Admin}
	a2 := auth.User{ID: admin2, Role: auth.Admin}
	r := auth.User{ID: researcher, Role: auth.Researcher}
	v := auth.User{ID: reviewer, Role: auth.Reviewer}
	project, err := store.CreateProject(ctx, a1, "speech-1", "Speech One", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.ListTiers(ctx, a2, project.ID); !errors.Is(err, ErrDenied) {
		t.Fatalf("cross-institution access should fail: %v", err)
	}
	if err = store.AddMember(ctx, a1, project.ID, researcher, auth.Reviewer); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mismatched role should fail: %v", err)
	}
	if err = store.AddMember(ctx, a1, project.ID, researcher, auth.Researcher); err != nil {
		t.Fatal(err)
	}
	if err = store.AddMember(ctx, a1, project.ID, reviewer, auth.Reviewer); err != nil {
		t.Fatal(err)
	}
	tier, err := store.CreateTier(ctx, a1, project.ID, Tier{Name: "POSI", Type: "controlled_vocabulary", Color: "#39745a", Required: true, Values: []string{"A", "B"}})
	if err != nil {
		t.Fatal(err)
	}
	recording, err := store.CreateRecording(ctx, a1, project.ID, Recording{Filename: "sample.wav", MediaType: "audio/wav", SizeBytes: 1000, DurationMS: 5000, ResearcherID: &researcher, ReviewerID: &reviewer})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SaveAnnotation(ctx, r, Annotation{RecordingID: recording.ID, TierID: tier.ID, StartMS: 0, EndMS: 6000, Value: "A"}, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("out-of-range annotation should fail: %v", err)
	}
	if _, err = store.SaveAnnotation(ctx, r, Annotation{RecordingID: recording.ID, TierID: tier.ID, StartMS: 0, EndMS: 1000, Value: "C"}, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown vocabulary should fail: %v", err)
	}
	annotation, err := store.SaveAnnotation(ctx, r, Annotation{RecordingID: recording.ID, TierID: tier.ID, StartMS: 0, EndMS: 1000, Value: "A"}, true)
	if err != nil {
		t.Fatal(err)
	}
	stale := annotation
	stale.Version = 99
	if _, err = store.SaveAnnotation(ctx, r, stale, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update should conflict: %v", err)
	}
	if err = store.Submit(ctx, r, recording.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.SaveAnnotation(ctx, r, annotation, false); !errors.Is(err, ErrDenied) {
		t.Fatalf("submitted work should be locked: %v", err)
	}
	if err = store.Review(ctx, v, recording.ID, "changes_requested", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("change request without comment should fail: %v", err)
	}
	if err = store.Review(ctx, v, recording.ID, "changes_requested", "Please revise"); err != nil {
		t.Fatal(err)
	}
}
