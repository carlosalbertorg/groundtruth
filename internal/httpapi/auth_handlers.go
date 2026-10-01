package httpapi

import (
	"database/sql"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/carlosalbertorg/groundtruth/internal/auth"
	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
)

type authHandlers struct {
	queries  *sqlc.Queries
	sessions *auth.SessionManager
	secure   bool // whether to mark the session cookie Secure (see router.go)
}

func newAuthHandlers(queries *sqlc.Queries, sessions *auth.SessionManager, secure bool) *authHandlers {
	return &authHandlers{queries: queries, sessions: sessions, secure: secure}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *authHandlers) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	user, err := h.queries.GetUserByEmail(r.Context(), email)
	if err != nil {
		// Same response whether the email doesn't exist or the password
		// is wrong, so a login attempt can't be used to enumerate which
		// email addresses have accounts.
		writeJSONError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}

	if !auth.VerifyPassword(user.PasswordHash, req.Password) {
		writeJSONError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}

	token, expiresAt, err := h.sessions.Create(r.Context(), user.ID, ip, r.UserAgent())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	_ = h.queries.UpdateUserLastLogin(r.Context(), sqlc.UpdateUserLastLoginParams{
		LastLoginAt: sql.NullTime{Time: time.Now(), Valid: true},
		ID:          user.ID,
	})

	http.SetCookie(w, h.sessionCookie(token, expiresAt))
	writeJSON(w, http.StatusOK, map[string]string{"id": user.ID, "email": user.Email})
}

func (h *authHandlers) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
		_ = h.sessions.Revoke(r.Context(), cookie.Value)
	}
	http.SetCookie(w, h.expiredSessionCookie())
	w.WriteHeader(http.StatusNoContent)
}

func (h *authHandlers) me(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": user.ID, "email": user.Email})
}

func (h *authHandlers) sessionCookie(token string, expiresAt time.Time) *http.Cookie {
	// HttpOnly, Secure, and SameSite are all set below; gosec's check
	// can't resolve Secure's value through h.secure, which is
	// intentionally conditional (see config.Config.SecureCookies).
	return &http.Cookie{ // #nosec G124
		Name:     auth.SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteLaxMode,
	}
}

func (h *authHandlers) expiredSessionCookie() *http.Cookie {
	// Same false positive as sessionCookie above.
	return &http.Cookie{ // #nosec G124
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteLaxMode,
	}
}
