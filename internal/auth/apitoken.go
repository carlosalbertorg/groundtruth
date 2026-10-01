package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
)

// APITokenPrefix makes a token recognizable at a glance (in logs, in a
// secrets manager, in a git-history diff someone's grepping) without
// revealing anything about its value.
const APITokenPrefix = "gt_"

// APITokenManager issues and validates personal API tokens, used to
// trigger a "check now" from CI without a browser session. Tokens are
// opaque; only their hash is ever persisted, same rationale as sessions.
type APITokenManager struct {
	queries *sqlc.Queries
	now     func() time.Time
}

// NewAPITokenManager builds an APITokenManager backed by queries.
func NewAPITokenManager(queries *sqlc.Queries) *APITokenManager {
	return &APITokenManager{queries: queries, now: time.Now}
}

// Create issues a new token for userID and returns its raw value. This
// is the only time the raw value exists outside the requester's own
// hands - it is never shown again.
func (m *APITokenManager) Create(ctx context.Context, userID, id, name string) (string, error) {
	raw, err := generateToken()
	if err != nil {
		return "", err
	}
	token := APITokenPrefix + raw

	_, err = m.queries.CreateAPIToken(ctx, sqlc.CreateAPITokenParams{
		ID:        id,
		UserID:    userID,
		Name:      name,
		TokenHash: hashToken(token),
		CreatedAt: m.now(),
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

// Validate looks up token and reports the ApiToken row if it's active
// (exists, not revoked), touching its last_used_at. Like sessions, it
// doesn't distinguish "doesn't exist" from "revoked" to the caller.
func (m *APITokenManager) Validate(ctx context.Context, token string) (sqlc.ApiToken, error) {
	t, err := m.queries.GetActiveAPITokenByHash(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlc.ApiToken{}, ErrSessionInvalid
		}
		return sqlc.ApiToken{}, fmt.Errorf("look up api token: %w", err)
	}

	now := sql.NullTime{Time: m.now(), Valid: true}
	if err := m.queries.TouchAPIToken(ctx, sqlc.TouchAPITokenParams{
		LastUsedAt: now,
		ID:         t.ID,
	}); err != nil {
		return sqlc.ApiToken{}, fmt.Errorf("touch api token: %w", err)
	}
	t.LastUsedAt = now
	return t, nil
}

// Revoke marks the token identified by id as revoked, scoped to userID
// so one user can't revoke another's token.
func (m *APITokenManager) Revoke(ctx context.Context, userID, id string) error {
	return m.queries.RevokeAPIToken(ctx, sqlc.RevokeAPITokenParams{
		RevokedAt: sql.NullTime{Time: m.now(), Valid: true},
		ID:        id,
		UserID:    userID,
	})
}
