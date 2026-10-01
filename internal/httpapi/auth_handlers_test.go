package httpapi_test

import (
	"net/http"
	"testing"
)

func TestLoginDoesNotRevealWhetherAnEmailHasAnAccount(t *testing.T) {
	r := newTestRouter(t)
	loginAsNewAdmin(t, r)

	unknownEmail := doJSON(t, r, http.MethodPost, "/api/auth/login", map[string]string{
		"email":    "nobody@example.com",
		"password": "some-password-here",
	})
	wrongPassword := doJSON(t, r, http.MethodPost, "/api/auth/login", map[string]string{
		"email":    "admin@example.com",
		"password": "not-the-right-password",
	})

	if unknownEmail.Code != http.StatusUnauthorized || wrongPassword.Code != http.StatusUnauthorized {
		t.Fatalf("statuses = (%d, %d), want both %d", unknownEmail.Code, wrongPassword.Code, http.StatusUnauthorized)
	}
	if unknownEmail.Body.String() != wrongPassword.Body.String() {
		t.Errorf("unknown-email body %q differs from wrong-password body %q: the response distinguishes the two",
			unknownEmail.Body.String(), wrongPassword.Body.String())
	}
}
