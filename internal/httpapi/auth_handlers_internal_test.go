package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/carlosalbertorg/groundtruth/internal/auth"
	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
	"github.com/carlosalbertorg/groundtruth/internal/store/storetest"
)

// An unknown email skips the bcrypt comparison a wrong password pays for,
// so without a stand-in cost the response time would reveal which email
// addresses have accounts. Time can't be asserted on reliably, so this checks
// the mechanism instead: the stand-in runs exactly when the email is unknown.
func TestLoginSpendsAPasswordCheckOnlyWhenTheEmailIsUnknown(t *testing.T) {
	queries := sqlc.New(storetest.OpenDB(t))

	hash, err := auth.HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, err := queries.CreateUser(context.Background(), sqlc.CreateUserParams{
		ID: uuid.NewString(), Email: "admin@example.com", PasswordHash: hash, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	h := newAuthHandlers(queries, auth.NewSessionManager(queries), false)
	burned := 0
	h.burnPasswordCheck = func(string) { burned++ }

	login := func(email, password string) int {
		body, err := json.Marshal(map[string]string{"email": email, "password": password})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		h.login(rec, req)
		return rec.Code
	}

	if code := login("nobody@example.com", "some-password"); code != http.StatusUnauthorized {
		t.Fatalf("unknown email: status = %d, want %d", code, http.StatusUnauthorized)
	}
	if burned != 1 {
		t.Errorf("stand-in password checks after an unknown email = %d, want 1", burned)
	}

	// A real account with a wrong password already pays for a bcrypt
	// comparison, so it must not pay twice.
	if code := login("admin@example.com", "not-the-password"); code != http.StatusUnauthorized {
		t.Fatalf("wrong password: status = %d, want %d", code, http.StatusUnauthorized)
	}
	if burned != 1 {
		t.Errorf("stand-in password checks after a wrong password = %d, want still 1", burned)
	}
}
