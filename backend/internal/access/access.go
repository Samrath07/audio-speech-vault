package access

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shivam3746/audio-speech-vault/internal/auth"
	platformmail "github.com/shivam3746/audio-speech-vault/internal/platform/mail"
	"golang.org/x/crypto/bcrypt"
)

const tokenLifetime = 24 * time.Hour

var (
	ErrNotFound      = errors.New("access request not found")
	ErrInvalidState  = errors.New("access request is not in the required state")
	ErrInvalidToken  = errors.New("invalid or expired token")
	ErrDuplicate     = errors.New("an account or open request already exists for this email")
	ErrPersonalEmail = errors.New("use your institutional email address")
)

type Request struct {
	ID              string    `json:"id"`
	InstitutionName string    `json:"institutionName"`
	Email           string    `json:"email"`
	DisplayName     string    `json:"displayName"`
	RequestedRole   auth.Role `json:"requestedRole"`
	Status          string    `json:"status"`
	RejectionReason *string   `json:"rejectionReason,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
}

type Service struct {
	db             *pgxpool.Pool
	mail           platformmail.Sender
	frontendOrigin string
}

func NewService(db *pgxpool.Pool, sender platformmail.Sender, frontendOrigin string) *Service {
	return &Service{db: db, mail: sender, frontendOrigin: strings.TrimRight(frontendOrigin, "/")}
}

func (s *Service) Create(ctx context.Context, institutionName, displayName, email string, role auth.Role) error {
	institutionName = strings.TrimSpace(institutionName)
	displayName = strings.TrimSpace(displayName)
	email, _, err := normalizeInstitutionalEmail(email)
	if err != nil {
		return err
	}
	if institutionName == "" || len(institutionName) > 160 || displayName == "" || len(displayName) > 100 {
		return errors.New("institution and name are required")
	}
	if role != auth.Admin && role != auth.Researcher && role != auth.Reviewer {
		return errors.New("invalid requested role")
	}
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE LOWER(email)=$1)`, email).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrDuplicate
	}
	token, hash, err := newToken()
	if err != nil {
		return err
	}
	var id string
	err = s.db.QueryRow(ctx, `INSERT INTO access_requests
		(institution_name, email, display_name, requested_role, status, verification_token_hash, verification_expires_at)
		VALUES ($1,$2,$3,$4,'email_pending',$5,$6) RETURNING id`, institutionName, email, displayName, role, hash, time.Now().Add(tokenLifetime)).Scan(&id)
	if uniqueViolation(err) {
		return ErrDuplicate
	}
	if err != nil {
		return err
	}
	verificationURL := s.frontendOrigin + "/?verify=" + token
	message := platformmail.Message{To: email, Subject: "Verify your Audio Speech Vault access request", Body: fmt.Sprintf("Verify your %s access request for %s:\n\n%s\n\nThis link expires in 24 hours.", role, institutionName, verificationURL)}
	if err := s.mail.Send(message); err != nil {
		_, _ = s.db.Exec(ctx, `DELETE FROM access_requests WHERE id=$1 AND status='email_pending'`, id)
		return fmt.Errorf("send verification email: %w", err)
	}
	return nil
}

func (s *Service) Verify(ctx context.Context, token string) error {
	command, err := s.db.Exec(ctx, `UPDATE access_requests SET status='pending', verification_token_hash=NULL,
		verification_expires_at=NULL, updated_at=NOW()
		WHERE verification_token_hash=$1 AND status='email_pending' AND verification_expires_at>NOW()`, tokenHash(token))
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrInvalidToken
	}
	return nil
}

func (s *Service) List(ctx context.Context) ([]Request, error) {
	if _, err := s.db.Exec(ctx, `UPDATE access_requests SET status='expired', updated_at=NOW()
		WHERE status='email_pending' AND verification_expires_at<=NOW()`); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT id,institution_name,email,display_name,requested_role,status,rejection_reason,created_at
		FROM access_requests ORDER BY CASE status WHEN 'pending' THEN 0 ELSE 1 END, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	requests := make([]Request, 0)
	for rows.Next() {
		var request Request
		if err := rows.Scan(&request.ID, &request.InstitutionName, &request.Email, &request.DisplayName, &request.RequestedRole, &request.Status, &request.RejectionReason, &request.CreatedAt); err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, rows.Err()
}

func (s *Service) Approve(ctx context.Context, requestID, actorID string, role auth.Role) error {
	if role != auth.Admin && role != auth.Researcher && role != auth.Reviewer {
		return errors.New("invalid approved role")
	}
	token, hash, err := newToken()
	if err != nil {
		return err
	}
	randomPassword, _, err := newToken()
	if err != nil {
		return err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(randomPassword), 12)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var request Request
	err = tx.QueryRow(ctx, `SELECT id,institution_name,email,display_name,requested_role,status,rejection_reason,created_at
		FROM access_requests WHERE id=$1 FOR UPDATE`, requestID).Scan(&request.ID, &request.InstitutionName, &request.Email, &request.DisplayName, &request.RequestedRole, &request.Status, &request.RejectionReason, &request.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if request.Status != "pending" {
		return ErrInvalidState
	}
	_, domain, err := normalizeInstitutionalEmail(request.Email)
	if err != nil {
		return err
	}
	var institutionID string
	err = tx.QueryRow(ctx, `INSERT INTO institutions(name,email_domain) VALUES($1,$2)
		ON CONFLICT(email_domain) DO UPDATE SET name=EXCLUDED.name,updated_at=NOW()
		RETURNING id`, request.InstitutionName, domain).Scan(&institutionID)
	if err != nil {
		return err
	}
	var userID string
	err = tx.QueryRow(ctx, `INSERT INTO users(email,display_name,password_hash,role,institution_id,must_change_password,approved_by,approved_at)
		VALUES($1,$2,$3,$4,$5,TRUE,$6,NOW()) RETURNING id`, request.Email, request.DisplayName, string(passwordHash), role, institutionID, actorID).Scan(&userID)
	if uniqueViolation(err) {
		return ErrDuplicate
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO password_setup_tokens(token_hash,user_id,expires_at) VALUES($1,$2,$3)`, hash, userID, time.Now().Add(tokenLifetime))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE access_requests SET status='approved',institution_id=$1,reviewed_by=$2,reviewed_at=NOW(),updated_at=NOW() WHERE id=$3`, institutionID, actorID, requestID)
	if err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, actorID, userID, "access_request.approved", map[string]any{"requestId": requestID, "requestedRole": request.RequestedRole, "approvedRole": role}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	setupURL := s.frontendOrigin + "/?setup=" + token
	return s.mail.Send(platformmail.Message{To: request.Email, Subject: "Your Audio Speech Vault access is approved", Body: fmt.Sprintf("Your access request was approved as %s. Set your password using this one-time link:\n\n%s\n\nThis link expires in 24 hours.", role, setupURL)})
}

func (s *Service) Reject(ctx context.Context, requestID, actorID, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 500 {
		return errors.New("rejection reason is required")
	}
	var email string
	err := s.db.QueryRow(ctx, `UPDATE access_requests SET status='rejected',reviewed_by=$1,reviewed_at=NOW(),rejection_reason=$2,updated_at=NOW()
		WHERE id=$3 AND status='pending' RETURNING email`, actorID, reason, requestID).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidState
	}
	if err != nil {
		return err
	}
	return s.mail.Send(platformmail.Message{To: email, Subject: "Audio Speech Vault access request update", Body: "Your access request was not approved.\n\nReason: " + reason})
}

func (s *Service) ResendSetup(ctx context.Context, requestID string) error {
	var email, userID string
	err := s.db.QueryRow(ctx, `SELECT ar.email,u.id FROM access_requests ar JOIN users u ON LOWER(u.email)=LOWER(ar.email)
		WHERE ar.id=$1 AND ar.status='approved' AND u.must_change_password AND u.is_active`, requestID).Scan(&email, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidState
	}
	if err != nil {
		return err
	}
	token, hash, err := newToken()
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM password_setup_tokens WHERE user_id=$1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO password_setup_tokens(token_hash,user_id,expires_at) VALUES($1,$2,$3)`, hash, userID, time.Now().Add(tokenLifetime)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	setupURL := s.frontendOrigin + "/?setup=" + token
	return s.mail.Send(platformmail.Message{To: email, Subject: "Set your Audio Speech Vault password", Body: "Set your password using this one-time link:\n\n" + setupURL + "\n\nThis link expires in 24 hours."})
}

func (s *Service) CompleteSetup(ctx context.Context, token, password string) error {
	if len(password) < 12 || len(password) > 72 {
		return errors.New("password must be 12 to 72 bytes")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID string
	err = tx.QueryRow(ctx, `SELECT user_id FROM password_setup_tokens
		WHERE token_hash=$1 AND used_at IS NULL AND expires_at>NOW() FOR UPDATE`, tokenHash(token)).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidToken
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET password_hash=$1,must_change_password=FALSE,updated_at=NOW() WHERE id=$2 AND is_active`, string(hash), userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE password_setup_tokens SET used_at=NOW() WHERE token_hash=$1`, tokenHash(token)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func normalizeInstitutionalEmail(value string) (string, string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value {
		return "", "", errors.New("invalid email address")
	}
	parts := strings.Split(value, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", errors.New("invalid email address")
	}
	domain := strings.TrimSuffix(parts[1], ".")
	if personalDomains[domain] {
		return "", "", ErrPersonalEmail
	}
	return value, domain, nil
}

var personalDomains = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "outlook.com": true, "hotmail.com": true,
	"live.com": true, "yahoo.com": true, "icloud.com": true, "proton.me": true,
	"protonmail.com": true, "aol.com": true,
}

func newToken() (string, []byte, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	return token, tokenHash(token), nil
}

func tokenHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func uniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func insertAudit(ctx context.Context, tx pgx.Tx, actor, target, action string, metadata map[string]any) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit_events(actor_user_id,action,entity_type,entity_id,metadata)
		VALUES($1,$2,'user',$3,$4)`, actor, action, target, metadata)
	return err
}
