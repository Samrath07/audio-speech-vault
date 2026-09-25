package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRoles(t *testing.T) {
	for _, role := range []Role{Superadmin, Admin, Researcher, Reviewer} {
		if !validRole(role) {
			t.Fatalf("role %q should be valid", role)
		}
	}
	if validRole("owner") {
		t.Fatal("unknown role should be rejected")
	}
}

func TestPasswordAndSessionPrimitives(t *testing.T) {
	if err := validatePassword("short"); err == nil {
		t.Fatal("short password should be rejected")
	}
	hash, err := hashPassword("a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if comparePassword(hash, "a-long-test-password") != nil || comparePassword(hash, "wrong-password") == nil {
		t.Fatal("password comparison failed")
	}
	first, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := randomToken()
	if err != nil || first == second || len(first) != 43 {
		t.Fatal("session tokens should be unique and 256-bit")
	}
	if !validCSRF(first, first) || validCSRF(first, second) || validCSRF(first, "") {
		t.Fatal("CSRF comparison failed")
	}
}

func TestSessionCookie(t *testing.T) {
	production := sessionCookie("token", true, 3600)
	if production.Name != "__Host-asv_session" || !production.Secure || !production.HttpOnly || production.Path != "/" || production.SameSite != http.SameSiteStrictMode {
		t.Fatalf("unsafe production cookie: %+v", production)
	}
	development := sessionCookie("token", false, 3600)
	if development.Name != "asv_session" || development.Secure {
		t.Fatalf("unexpected development cookie: %+v", development)
	}
}

func TestProxiedFrontendOrigin(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "http://app:8080/api/auth/login", nil)
	request.Header.Set("Origin", "http://localhost:5173")
	request.Header.Set("X-Requested-With", "AudioSpeechVault")
	if !validMutationRequest(httptest.NewRecorder(), request, "http://localhost:5173") {
		t.Fatal("configured dashboard origin should be accepted")
	}
	request.Header.Set("Origin", "http://attacker.example")
	if validMutationRequest(httptest.NewRecorder(), request, "http://localhost:5173") {
		t.Fatal("untrusted origin should be rejected")
	}
}
