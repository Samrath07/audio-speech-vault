package access

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shivam3746/audio-speech-vault/internal/auth"
	platformmail "github.com/shivam3746/audio-speech-vault/internal/platform/mail"
	"golang.org/x/crypto/bcrypt"
)

func TestInstitutionalEmailValidation(t *testing.T) {
	if _, _, err := normalizeInstitutionalEmail("person@gmail.com"); !errors.Is(err, ErrPersonalEmail) {
		t.Fatalf("personal domain should be rejected: %v", err)
	}
	email, domain, err := normalizeInstitutionalEmail("Researcher@University.edu")
	if err != nil || email != "researcher@university.edu" || domain != "university.edu" {
		t.Fatalf("unexpected normalization: %q %q %v", email, domain, err)
	}
}

func TestAccessRequestLifecycle(t *testing.T) {
	databaseURL := os.Getenv("TEST_ACCESS_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_ACCESS_DATABASE_URL not set")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil || !strings.HasSuffix(config.ConnConfig.Database, "_access_test") {
		t.Fatal("integration test requires a dedicated database ending in _access_test")
	}
	db, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	clean := func() {
		_, err := db.Exec(context.Background(), `TRUNCATE password_setup_tokens,access_requests,sessions,audit_events,project_members,projects,users,institutions CASCADE`)
		if err != nil {
			t.Fatal(err)
		}
	}
	clean()
	t.Cleanup(func() { clean(); db.Close() })
	if err := auth.BootstrapSuperadmin(context.Background(), db, "root@vault.test", "Root", "root-password-123"); err != nil {
		t.Fatal(err)
	}
	var actorID string
	if err := db.QueryRow(context.Background(), `SELECT id FROM users WHERE email='root@vault.test'`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	sender := &captureSender{}
	service := NewService(db, sender, "http://localhost:5173")
	if err := service.Create(context.Background(), "Personal", "Person", "person@gmail.com", auth.Researcher); !errors.Is(err, ErrPersonalEmail) {
		t.Fatalf("personal address should fail: %v", err)
	}
	if err := service.Create(context.Background(), "Example University", "New Researcher", "person@example.edu", auth.Researcher); err != nil {
		t.Fatal(err)
	}
	verificationToken := tokenFromMessage(t, sender.last().Body, "?verify=")
	if err := service.Verify(context.Background(), verificationToken); err != nil {
		t.Fatal(err)
	}
	if err := service.Verify(context.Background(), verificationToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("verification token reuse should fail: %v", err)
	}
	requests, err := service.List(context.Background())
	if err != nil || len(requests) != 1 || requests[0].Status != "pending" {
		t.Fatalf("unexpected request list: %+v %v", requests, err)
	}
	if err := service.Approve(context.Background(), requests[0].ID, actorID, auth.Reviewer); err != nil {
		t.Fatal(err)
	}
	setupToken := tokenFromMessage(t, sender.last().Body, "?setup=")
	if err := service.CompleteSetup(context.Background(), setupToken, "new-password-12345"); err != nil {
		t.Fatal(err)
	}
	if err := service.CompleteSetup(context.Background(), setupToken, "another-password-123"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("setup token reuse should fail: %v", err)
	}
	var role auth.Role
	var mustChange bool
	var passwordHash, institutionDomain string
	if err := db.QueryRow(context.Background(), `SELECT u.role,u.must_change_password,u.password_hash,i.email_domain
		FROM users u JOIN institutions i ON i.id=u.institution_id WHERE u.email='person@example.edu'`).Scan(&role, &mustChange, &passwordHash, &institutionDomain); err != nil {
		t.Fatal(err)
	}
	if role != auth.Reviewer || mustChange || institutionDomain != "example.edu" || bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte("new-password-12345")) != nil {
		t.Fatalf("approved account was not finalized correctly")
	}
	if err := service.Create(context.Background(), "Other University", "Rejected User", "rejected@other.edu", auth.Admin); err != nil {
		t.Fatal(err)
	}
	if err := service.Verify(context.Background(), tokenFromMessage(t, sender.last().Body, "?verify=")); err != nil {
		t.Fatal(err)
	}
	requests, err = service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var rejectedID string
	for _, request := range requests {
		if request.Email == "rejected@other.edu" {
			rejectedID = request.ID
		}
	}
	if rejectedID == "" {
		t.Fatal("verified request missing")
	}
	if err := service.Reject(context.Background(), rejectedID, actorID, "Institution could not be verified"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sender.last().Body, "Institution could not be verified") {
		t.Fatal("rejection email did not include the reason")
	}
}

type captureSender struct {
	mu       sync.Mutex
	messages []platformmail.Message
}

func (s *captureSender) Send(message platformmail.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, message)
	return nil
}

func (s *captureSender) last() platformmail.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.messages[len(s.messages)-1]
}

func tokenFromMessage(t *testing.T, body, marker string) string {
	t.Helper()
	index := strings.Index(body, marker)
	if index < 0 {
		t.Fatalf("token marker missing from message")
	}
	value := body[index+len(marker):]
	if newline := strings.IndexByte(value, '\n'); newline >= 0 {
		value = value[:newline]
	}
	return strings.TrimSpace(value)
}
