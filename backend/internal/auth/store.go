package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errLastSuperadmin = errors.New("cannot remove the last active superadmin")

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

func (s *Store) findUserByEmail(ctx context.Context, email string) (User, string, error) {
	var user User
	var passwordHash string
	err := s.db.QueryRow(ctx, `SELECT id, email, display_name, role, is_active, must_change_password, password_hash
		FROM users WHERE LOWER(email) = $1`, email).Scan(
		&user.ID, &user.Email, &user.DisplayName, &user.Role, &user.IsActive, &user.MustChangePassword, &passwordHash,
	)
	return user, passwordHash, err
}

func (s *Store) createSession(ctx context.Context, userID string) (string, string, error) {
	token, err := randomToken()
	if err != nil {
		return "", "", err
	}
	csrf, err := randomToken()
	if err != nil {
		return "", "", err
	}
	_, _ = s.db.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= NOW()`)
	_, err = s.db.Exec(ctx, `INSERT INTO sessions (token_hash, user_id, csrf_token, expires_at)
		VALUES ($1, $2, $3, $4)`, tokenHash(token), userID, csrf, time.Now().Add(sessionLifetime))
	return token, csrf, err
}

func (s *Store) getSession(ctx context.Context, token string) (session, error) {
	var current session
	current.tokenHash = tokenHash(token)
	err := s.db.QueryRow(ctx, `SELECT u.id, u.email, u.display_name, u.role, u.is_active, u.must_change_password, s.csrf_token
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > NOW() AND u.is_active`, current.tokenHash).Scan(
		&current.user.ID, &current.user.Email, &current.user.DisplayName,
		&current.user.Role, &current.user.IsActive, &current.user.MustChangePassword, &current.csrfToken,
	)
	return current, err
}

func (s *Store) deleteSession(ctx context.Context, hash []byte) error {
	_, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, hash)
	return err
}

func (s *Store) changePassword(ctx context.Context, userID, oldPassword, newPassword string) error {
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var existing string
	err = tx.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1 AND is_active FOR UPDATE`, userID).Scan(&existing)
	if err != nil {
		return err
	}
	if err := comparePassword(existing, oldPassword); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2`, hash, userID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, userID, userID, "user.password_changed", nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) listUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.Query(ctx, `SELECT id, email, display_name, role, is_active FROM users ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.Email, &user.DisplayName, &user.Role, &user.IsActive); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Store) createUser(ctx context.Context, actor string, user User, password string) (User, error) {
	hash, err := hashPassword(password)
	if err != nil {
		return User{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `INSERT INTO users (email, display_name, password_hash, role)
		VALUES ($1, $2, $3, $4) RETURNING id, is_active`, user.Email, user.DisplayName, hash, user.Role).Scan(&user.ID, &user.IsActive)
	if err != nil {
		return User{}, err
	}
	if err := insertAudit(ctx, tx, actor, user.ID, "user.created", map[string]any{"role": user.Role}); err != nil {
		return User{}, err
	}
	return user, tx.Commit(ctx)
}

func (s *Store) updateUser(ctx context.Context, actor, targetID string, role *Role, active *bool) (User, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7912345)`); err != nil {
		return User{}, err
	}
	var user User
	err = tx.QueryRow(ctx, `SELECT id, email, display_name, role, is_active FROM users WHERE id = $1 FOR UPDATE`, targetID).Scan(
		&user.ID, &user.Email, &user.DisplayName, &user.Role, &user.IsActive,
	)
	if err != nil {
		return User{}, err
	}
	previousRole, previousActive := user.Role, user.IsActive
	if role != nil {
		user.Role = *role
	}
	if active != nil {
		user.IsActive = *active
	}
	if previousRole == Superadmin && previousActive && (user.Role != Superadmin || !user.IsActive) {
		var count int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE role = 'superadmin' AND is_active`).Scan(&count); err != nil {
			return User{}, err
		}
		if count <= 1 {
			return User{}, errLastSuperadmin
		}
	}
	_, err = tx.Exec(ctx, `UPDATE users SET role = $1, is_active = $2, updated_at = NOW() WHERE id = $3`, user.Role, user.IsActive, user.ID)
	if err != nil {
		return User{}, err
	}
	if previousRole != user.Role || previousActive != user.IsActive {
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, user.ID); err != nil {
			return User{}, err
		}
		metadata := map[string]any{"previousRole": previousRole, "newRole": user.Role, "previousActive": previousActive, "newActive": user.IsActive}
		if err := insertAudit(ctx, tx, actor, user.ID, "user.updated", metadata); err != nil {
			return User{}, err
		}
	}
	return user, tx.Commit(ctx)
}

func (s *Store) resetPassword(ctx context.Context, actor, targetID, password string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	command, err := tx.Exec(ctx, `UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2`, hash, targetID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, targetID); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, actor, targetID, "user.password_reset", nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func insertAudit(ctx context.Context, tx pgx.Tx, actor, target, action string, metadata map[string]any) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode audit metadata: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events (actor_user_id, action, entity_type, entity_id, metadata)
		VALUES ($1, $2, 'user', $3, $4::jsonb)`, actor, action, target, string(encoded))
	return err
}
