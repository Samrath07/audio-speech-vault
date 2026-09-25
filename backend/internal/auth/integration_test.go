package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAuthenticationFlow(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil || !strings.HasSuffix(config.ConnConfig.Database, "_auth_test") {
		t.Fatal("integration test requires a dedicated database ending in _auth_test")
	}
	db, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	clean := func() {
		if _, err := db.Exec(context.Background(), `TRUNCATE sessions, audit_events, project_members, projects, users CASCADE`); err != nil {
			t.Fatal(err)
		}
	}
	clean()
	t.Cleanup(func() { clean(); db.Close() })

	if err := BootstrapSuperadmin(context.Background(), db, "root@example.test", "Root User", "root-password-123"); err != nil {
		t.Fatal(err)
	}
	if err := BootstrapSuperadmin(context.Background(), db, "again@example.test", "Again", "another-password-123"); !errors.Is(err, ErrAlreadyBootstrapped) {
		t.Fatalf("second bootstrap should fail: %v", err)
	}
	handler, err := NewHandler(NewStore(db), slog.New(slog.NewTextHandler(io.Discard, nil)), false, "")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handler.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	rootClient := newTestClient(t)
	requestStatus(t, rootClient, server.URL, "POST", "/api/auth/login", map[string]string{"email": "root@example.test", "password": "wrong-password"}, "", http.StatusUnauthorized)
	root := loginTestUser(t, rootClient, server.URL, "root@example.test", "root-password-123", Superadmin)
	requestStatus(t, rootClient, server.URL, "GET", "/api/auth/me", nil, "", http.StatusOK)
	requestStatus(t, rootClient, server.URL, "POST", "/api/users", map[string]any{}, "", http.StatusForbidden)
	requestStatus(t, rootClient, server.URL, "PATCH", "/api/users/invalid", map[string]any{}, root.CSRFToken, http.StatusNotFound)
	requestStatus(t, rootClient, server.URL, "PATCH", "/api/users/"+root.User.ID, map[string]any{"role": "admin"}, root.CSRFToken, http.StatusConflict)

	accounts := []struct {
		role  Role
		email string
	}{
		{Admin, "admin@example.test"},
		{Researcher, "researcher@example.test"},
		{Reviewer, "reviewer@example.test"},
	}
	users := make(map[Role]User)
	clients := make(map[Role]*http.Client)
	sessions := make(map[Role]testSession)
	for _, account := range accounts {
		var user User
		response := requestStatus(t, rootClient, server.URL, "POST", "/api/users", map[string]any{
			"email": account.email, "displayName": string(account.role), "role": account.role, "password": "temporary-password-123",
		}, root.CSRFToken, http.StatusCreated)
		decodeResponse(t, response, &user)
		users[account.role] = user
		client := newTestClient(t)
		clients[account.role] = client
		sessions[account.role] = loginTestUser(t, client, server.URL, account.email, "temporary-password-123", account.role)
		requestStatus(t, client, server.URL, "GET", "/api/users", nil, "", http.StatusForbidden)
		requestStatus(t, client, server.URL, "POST", "/api/users", map[string]any{"email": "other@example.test"}, sessions[account.role].CSRFToken, http.StatusForbidden)
	}

	researcher := users[Researcher]
	requestStatus(t, rootClient, server.URL, "PATCH", "/api/users/"+researcher.ID, map[string]any{"role": "admin"}, root.CSRFToken, http.StatusOK)
	requestStatus(t, clients[Researcher], server.URL, "GET", "/api/auth/me", nil, "", http.StatusUnauthorized)
	researcherSession := loginTestUser(t, clients[Researcher], server.URL, researcher.Email, "temporary-password-123", Admin)
	requestStatus(t, clients[Researcher], server.URL, "POST", "/api/auth/change-password", map[string]string{
		"currentPassword": "wrong-password", "newPassword": "new-password-12345",
	}, researcherSession.CSRFToken, http.StatusBadRequest)
	requestStatus(t, clients[Researcher], server.URL, "POST", "/api/auth/change-password", map[string]string{
		"currentPassword": "temporary-password-123", "newPassword": "new-password-12345",
	}, researcherSession.CSRFToken, http.StatusNoContent)
	requestStatus(t, clients[Researcher], server.URL, "GET", "/api/auth/me", nil, "", http.StatusUnauthorized)
	loginTestUser(t, newTestClient(t), server.URL, researcher.Email, "new-password-12345", Admin)

	reviewer := users[Reviewer]
	requestStatus(t, rootClient, server.URL, "PATCH", "/api/users/"+reviewer.ID, map[string]any{"isActive": false}, root.CSRFToken, http.StatusOK)
	requestStatus(t, clients[Reviewer], server.URL, "GET", "/api/auth/me", nil, "", http.StatusUnauthorized)
	requestStatus(t, newTestClient(t), server.URL, "POST", "/api/auth/login", map[string]string{"email": reviewer.Email, "password": "temporary-password-123"}, "", http.StatusUnauthorized)

	admin := users[Admin]
	requestStatus(t, rootClient, server.URL, "POST", "/api/users/"+admin.ID+"/password", map[string]string{"password": "reset-password-12345"}, root.CSRFToken, http.StatusNoContent)
	requestStatus(t, clients[Admin], server.URL, "GET", "/api/auth/me", nil, "", http.StatusUnauthorized)
	loginTestUser(t, newTestClient(t), server.URL, admin.Email, "reset-password-12345", Admin)

	requestStatus(t, rootClient, server.URL, "POST", "/api/auth/logout", nil, root.CSRFToken, http.StatusNoContent)
	requestStatus(t, rootClient, server.URL, "GET", "/api/auth/me", nil, "", http.StatusUnauthorized)
}

type testSession struct {
	User      User   `json:"user"`
	CSRFToken string `json:"csrfToken"`
}

func newTestClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}

func loginTestUser(t *testing.T, client *http.Client, base, email, password string, role Role) testSession {
	t.Helper()
	response := requestStatus(t, client, base, "POST", "/api/auth/login", map[string]string{"email": email, "password": password}, "", http.StatusOK)
	var session testSession
	decodeResponse(t, response, &session)
	if session.User.Role != role || session.CSRFToken == "" {
		t.Fatalf("unexpected login session for %s", email)
	}
	return session
}

func requestStatus(t *testing.T, client *http.Client, base, method, path string, payload any, csrf string, expected int) []byte {
	t.Helper()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, base+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if method != "GET" {
		request.Header.Set("Origin", base)
		request.Header.Set("X-Requested-With", "AudioSpeechVault")
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		if csrf != "" {
			request.Header.Set("X-CSRF-Token", csrf)
		}
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != expected {
		t.Fatalf("%s %s: got %d, want %d: %s", method, path, response.StatusCode, expected, contents)
	}
	return contents
}

func decodeResponse(t *testing.T, contents []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(contents, target); err != nil {
		t.Fatal(err)
	}
}
