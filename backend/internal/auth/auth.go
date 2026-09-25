package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const sessionLifetime = 12 * time.Hour
const passwordCost = 12

var errInvalidPassword = errors.New("incorrect password")

type Role string

const (
	Superadmin Role = "superadmin"
	Admin      Role = "admin"
	Researcher Role = "researcher"
	Reviewer   Role = "reviewer"
)

type User struct {
	ID                 string `json:"id"`
	Email              string `json:"email"`
	DisplayName        string `json:"displayName"`
	Role               Role   `json:"role"`
	IsActive           bool   `json:"isActive"`
	MustChangePassword bool   `json:"mustChangePassword"`
}

type session struct {
	user      User
	tokenHash []byte
	csrfToken string
}

func validRole(role Role) bool {
	switch role {
	case Superadmin, Admin, Researcher, Reviewer:
		return true
	default:
		return false
	}
}

func normalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 {
		return "", errors.New("invalid email address")
	}
	return email, nil
}

func validatePassword(password string) error {
	if len(password) < 12 || len(password) > 72 {
		return errors.New("password must be 12 to 72 bytes")
	}
	return nil
}

func hashPassword(password string) (string, error) {
	if err := validatePassword(password); err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), passwordCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

func comparePassword(hash, password string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return errInvalidPassword
	}
	return nil
}

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func tokenHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func validCSRF(expected, actual string) bool {
	return len(actual) == len(expected) && subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

func cookieName(secure bool) string {
	if secure {
		return "__Host-asv_session"
	}
	return "asv_session"
}

func sessionCookie(token string, secure bool, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     cookieName(secure),
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   maxAge,
	}
}
