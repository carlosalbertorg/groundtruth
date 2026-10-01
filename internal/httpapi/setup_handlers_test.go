package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/carlosalbertorg/groundtruth/internal/auth"
)

func TestSetupRejectsPasswordOverBcryptLimit(t *testing.T) {
	r := newTestRouter(t)

	rec := doJSON(t, r, http.MethodPost, "/api/setup/admin", map[string]string{
		"email":    "admin@example.com",
		"password": strings.Repeat("a", auth.MaxPasswordBytes+1),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body)
	}
	var errResp struct {
		Error string `json:"error"`
	}
	decodeBody(t, rec, &errResp)
	if errResp.Error != "password_too_long" {
		t.Errorf("error = %q, want password_too_long (not an opaque 500 from bcrypt)", errResp.Error)
	}

	// The rejected attempt must not have burned the one-time setup.
	statusRec := doJSON(t, r, http.MethodGet, "/api/setup/status", nil)
	var status struct {
		SetupComplete bool `json:"setup_complete"`
	}
	decodeBody(t, statusRec, &status)
	if status.SetupComplete {
		t.Error("setup reported complete after a rejected attempt")
	}
}

func TestSetupAcceptsPasswordAtBcryptLimit(t *testing.T) {
	r := newTestRouter(t)

	rec := doJSON(t, r, http.MethodPost, "/api/setup/admin", map[string]string{
		"email":    "admin@example.com",
		"password": strings.Repeat("a", auth.MaxPasswordBytes),
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body)
	}
}
