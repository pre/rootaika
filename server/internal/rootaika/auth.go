package rootaika

import (
	"context"
	"crypto/subtle"
	"net/http"
)

func (a *App) requireRole(w http.ResponseWriter, r *http.Request, allowed ...Role) (Role, bool) {
	role, ok, err := a.authenticate(r.Context(), r)
	if err != nil {
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return "", false
	}
	if !ok || !roleAllowed(role, allowed) {
		w.Header().Set("WWW-Authenticate", `Basic realm="rootaika"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	return role, true
}

func (a *App) authenticate(ctx context.Context, r *http.Request) (Role, bool, error) {
	username, password, ok := r.BasicAuth()
	if !ok {
		return "", false, nil
	}

	credentials, err := a.store.Credentials(ctx)
	if err != nil {
		return "", false, err
	}
	for _, credential := range credentials {
		if constantTimeEqual(username, credential.Username) && constantTimeEqual(password, credential.Password) {
			return credential.Role, true, nil
		}
	}
	return "", false, nil
}

func roleAllowed(role Role, allowed []Role) bool {
	for _, candidate := range allowed {
		if role == candidate {
			return true
		}
	}
	return false
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
