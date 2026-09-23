package auth

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrAlreadyBootstrapped = errors.New("users already exist; bootstrap is disabled")

func BootstrapSuperadmin(ctx context.Context, db *pgxpool.Pool, email, displayName, password string) error {
	var err error
	email, err = normalizeEmail(email)
	if err != nil {
		return err
	}
	if displayName == "" || len(displayName) > 100 {
		return errors.New("display name must be 1 to 100 characters")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7912345)`); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return ErrAlreadyBootstrapped
	}
	_, err = tx.Exec(ctx, `INSERT INTO users (email, display_name, password_hash, role)
		VALUES ($1, $2, $3, 'superadmin')`, email, displayName, hash)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
