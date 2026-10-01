package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
)

const (
	// SessionCookieName is the cookie groundtruth issues on login.
	SessionCookieName = "gt_session"

	// sessionTokenBytes is the raw entropy of a session token before
	// base64 encoding. 32 bytes (256 bits) is comfortably unguessable.
	sessionTokenBytes = 32

	// sessionIdleTimeout logs a session out after this long with no
	// requests, even if it hasn't hit its absolute lifetime yet.
	sessionIdleTimeout = 7 * 24 * time.Hour

	// sessionAbsoluteLifetime is a session's hard expiry from creation,
	// regardless of activity.
	sessionAbsoluteLifetime = 30 * 24 * time.Hour
)

// ErrSessionInvalid covers both an unrecognized token and one that has
// expired (absolute or idle) — callers don't get to distinguish those, so
// as not to leak which case applies.
var ErrSessionInvalid = errors.New("session invalid or expired")

// SessionManager creates, validates, and revokes sessions. Tokens are
// opaque random values; only their SHA-256 hash is ever persisted, so a
// database read alone can't be replayed as a live session.
type SessionManager struct {
	queries *sqlc.Queries
	now     func() time.Time
}

// NewSessionManager builds a SessionManager backed by queries.
func NewSessionManager(queries *sqlc.Queries) *SessionManager {
	return NewSessionManagerWithClock(queries, time.Now)
}

// NewSessionManagerWithClock is NewSessionManager with an injectable
// clock, so tests can exercise idle/absolute expiry without sleeping in
// real time.
func NewSessionManagerWithClock(queries *sqlc.Queries, now func() time.Time) *SessionManager {
	return &SessionManager{queries: queries, now: now}
}

// Create issues a new session for userID and returns the raw token to set
// in the session cookie, along with its expiry (for the cookie's Max-Age).
func (m *SessionManager) Create(ctx context.Context, userID, ipAddress, userAgent string) (token string, expiresAt time.Time, err error) {
	token, err = generateToken()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("generate session token: %w", err)
	}

	now := m.now()
	expiresAt = now.Add(sessionAbsoluteLifetime)

	err = m.queries.CreateSession(ctx, sqlc.CreateSessionParams{
		TokenHash:  hashToken(token),
		UserID:     userID,
		CreatedAt:  now,
		ExpiresAt:  expiresAt,
		LastSeenAt: now,
		IpAddress:  nullString(ipAddress),
		UserAgent:  nullString(userAgent),
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create session: %w", err)
	}
	return token, expiresAt, nil
}

// Validate looks up token, enforces both expiry rules, and — if the
// session is still live — updates its last-seen time. It returns
// ErrSessionInvalid for any reason the session can't be used, without
// distinguishing "doesn't exist" from "expired."
func (m *SessionManager) Validate(ctx context.Context, token string) (sqlc.Session, error) {
	sess, err := m.queries.GetSessionByTokenHash(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlc.Session{}, ErrSessionInvalid
		}
		return sqlc.Session{}, fmt.Errorf("look up session: %w", err)
	}

	now := m.now()
	if now.After(sess.ExpiresAt) || now.Sub(sess.LastSeenAt) > sessionIdleTimeout {
		_ = m.queries.DeleteSession(ctx, sess.TokenHash)
		return sqlc.Session{}, ErrSessionInvalid
	}

	if err := m.queries.TouchSession(ctx, sqlc.TouchSessionParams{
		LastSeenAt: now,
		TokenHash:  sess.TokenHash,
	}); err != nil {
		return sqlc.Session{}, fmt.Errorf("touch session: %w", err)
	}
	sess.LastSeenAt = now
	return sess, nil
}

// Revoke deletes the session identified by token (used on logout). It is
// a no-op if the token doesn't match a live session.
func (m *SessionManager) Revoke(ctx context.Context, token string) error {
	return m.queries.DeleteSession(ctx, hashToken(token))
}

func generateToken() (string, error) {
	b := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
