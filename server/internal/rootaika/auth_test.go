package rootaika

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthenticate(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()

	tests := []struct {
		name     string
		setAuth  bool
		username string
		password string
		wantRole Role
		wantOK   bool
	}{
		{name: "no header", setAuth: false, wantOK: false},
		{name: "wrong creds", setAuth: true, username: "admin", password: "nope", wantOK: false},
		{name: "admin", setAuth: true, username: "admin", password: "admin", wantRole: RoleAdmin, wantOK: true},
		{name: "client", setAuth: true, username: "client", password: "client", wantRole: RoleClient, wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.setAuth {
				request.SetBasicAuth(tt.username, tt.password)
			}
			role, ok, err := app.authenticate(ctx, request)
			if err != nil {
				t.Fatalf("authenticate err: %v", err)
			}
			if ok != tt.wantOK {
				t.Fatalf("ok = %v want %v", ok, tt.wantOK)
			}
			if ok && role != tt.wantRole {
				t.Fatalf("role = %q want %q", role, tt.wantRole)
			}
		})
	}
}

func TestRequireRoleAllowedAndForbidden(t *testing.T) {
	app := testApp(t)

	allowed := httptest.NewRequest(http.MethodGet, "/", nil)
	allowed.SetBasicAuth("admin", "admin")
	recorder := httptest.NewRecorder()
	role, ok := app.requireRole(recorder, allowed, RoleAdmin)
	if !ok || role != RoleAdmin {
		t.Fatalf("admin should be allowed: ok=%v role=%q", ok, role)
	}

	forbidden := httptest.NewRequest(http.MethodGet, "/", nil)
	forbidden.SetBasicAuth("client", "client")
	forbiddenRecorder := httptest.NewRecorder()
	if _, ok := app.requireRole(forbiddenRecorder, forbidden, RoleAdmin); ok {
		t.Fatalf("client should be forbidden for admin-only route")
	}
	if forbiddenRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("forbidden status = %d", forbiddenRecorder.Code)
	}
	if forbiddenRecorder.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("missing WWW-Authenticate header")
	}
}

func TestRoleAllowed(t *testing.T) {
	tests := []struct {
		name    string
		role    Role
		allowed []Role
		want    bool
	}{
		{name: "match", role: RoleAdmin, allowed: []Role{RoleAdmin, RoleClient}, want: true},
		{name: "no match", role: RoleClient, allowed: []Role{RoleAdmin}, want: false},
		{name: "empty allowed", role: RoleAdmin, allowed: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := roleAllowed(tt.role, tt.allowed); got != tt.want {
				t.Fatalf("roleAllowed = %v want %v", got, tt.want)
			}
		})
	}
}

func TestConstantTimeEqual(t *testing.T) {
	if !constantTimeEqual("secret", "secret") {
		t.Fatalf("equal strings should match")
	}
	if constantTimeEqual("secret", "other") {
		t.Fatalf("different strings should not match")
	}
	if constantTimeEqual("secret", "secre") {
		t.Fatalf("different length should not match")
	}
}
