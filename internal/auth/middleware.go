package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
)

type contextKey int

const (
	userContextKey contextKey = iota
	apiTokenContextKey
)

// RequireSession resolves the gt_session cookie into a user and makes it
// available via UserFromContext, or responds 401 if there's no valid
// session.
func RequireSession(sessions *SessionManager, queries *sqlc.Queries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(SessionCookieName)
			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			sess, err := sessions.Validate(r.Context(), cookie.Value)
			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			user, err := queries.GetUserByID(r.Context(), sess.UserID)
			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserFromContext returns the user attached by RequireSession, if any.
func UserFromContext(ctx context.Context) (sqlc.User, bool) {
	u, ok := ctx.Value(userContextKey).(sqlc.User)
	return u, ok
}

// RequireSessionOrAPIToken allows either a valid session cookie or a
// valid "Authorization: Bearer <token>" API token - for routes that
// need to work both from the browser and from an unattended caller like
// CI. On the token path it does not populate UserFromContext (a token is
// not a login session); a handler that needs to know which kind of caller
// it has can ask APITokenFromContext.
func RequireSessionOrAPIToken(sessions *SessionManager, tokens *APITokenManager, queries *sqlc.Queries) func(http.Handler) http.Handler {
	requireSession := RequireSession(sessions, queries)
	return func(next http.Handler) http.Handler {
		sessionWrapped := requireSession(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token, ok := bearerToken(r); ok {
				apiToken, err := tokens.Validate(r.Context(), token)
				if err != nil {
					writeJSONError(w, http.StatusUnauthorized, "unauthorized")
					return
				}
				ctx := context.WithValue(r.Context(), apiTokenContextKey, apiToken)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			sessionWrapped.ServeHTTP(w, r)
		})
	}
}

// APITokenFromContext returns the API token that authenticated the request,
// if RequireSessionOrAPIToken let it through on the token path rather than
// on a session cookie.
func APITokenFromContext(ctx context.Context) (sqlc.ApiToken, bool) {
	t, ok := ctx.Value(apiTokenContextKey).(sqlc.ApiToken)
	return t, ok
}

func bearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	return strings.TrimPrefix(h, prefix), true
}

// RequireSetupComplete blocks every route it wraps until the first admin
// account exists, responding 503 with "setup_required" otherwise. Mount
// the setup endpoints themselves outside of this middleware.
func RequireSetupComplete(gate *SetupGate) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			done, err := gate.Complete(r.Context())
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "internal_error")
				return
			}
			if !done {
				writeJSONError(w, http.StatusServiceUnavailable, "setup_required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeJSONError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
