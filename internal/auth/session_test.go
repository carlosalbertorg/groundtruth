package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/carlosalbertorg/groundtruth/internal/auth"
	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
	"github.com/carlosalbertorg/groundtruth/internal/store/storetest"
)

func createTestUser(t *testing.T, queries *sqlc.Queries) sqlc.User {
	t.Helper()
	user, err := queries.CreateUser(context.Background(), sqlc.CreateUserParams{
		ID:           uuid.NewString(),
		Email:        "test@example.com",
		PasswordHash: "irrelevant-for-this-test",
		CreatedAt:    time.Now(),
	})
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	return user
}

func TestSessionCreateAndValidate(t *testing.T) {
	queries := sqlc.New(storetest.OpenDB(t))
	user := createTestUser(t, queries)

	sessions := auth.NewSessionManager(queries)
	ctx := context.Background()

	token, expiresAt, err := sessions.Create(ctx, user.ID, "203.0.113.1", "test-agent")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if token == "" {
		t.Fatal("Create returned an empty token")
	}
	if !expiresAt.After(time.Now()) {
		t.Fatal("Create returned an expiry that's already in the past")
	}

	sess, err := sessions.Validate(ctx, token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if sess.UserID != user.ID {
		t.Errorf("Validate returned session for user %q, want %q", sess.UserID, user.ID)
	}
}

func TestSessionValidateRejectsUnknownToken(t *testing.T) {
	queries := sqlc.New(storetest.OpenDB(t))
	sessions := auth.NewSessionManager(queries)

	if _, err := sessions.Validate(context.Background(), "this-token-does-not-exist"); err != auth.ErrSessionInvalid {
		t.Errorf("Validate error = %v, want %v", err, auth.ErrSessionInvalid)
	}
}

func TestSessionRevoke(t *testing.T) {
	queries := sqlc.New(storetest.OpenDB(t))
	user := createTestUser(t, queries)
	sessions := auth.NewSessionManager(queries)
	ctx := context.Background()

	token, _, err := sessions.Create(ctx, user.ID, "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := sessions.Revoke(ctx, token); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if _, err := sessions.Validate(ctx, token); err != auth.ErrSessionInvalid {
		t.Errorf("Validate after Revoke error = %v, want %v", err, auth.ErrSessionInvalid)
	}
}

func TestSessionExpiresAfterAbsoluteLifetime(t *testing.T) {
	queries := sqlc.New(storetest.OpenDB(t))
	user := createTestUser(t, queries)
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := start
	sessions := auth.NewSessionManagerWithClock(queries, func() time.Time { return clock })

	token, _, err := sessions.Create(ctx, user.ID, "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Validate daily for 29 days, so last_seen_at is refreshed often
	// enough that the 7-day idle timeout never applies — isolating the
	// absolute-lifetime check from the idle check.
	for day := 1; day <= 29; day++ {
		clock = start.Add(time.Duration(day) * 24 * time.Hour)
		if _, err := sessions.Validate(ctx, token); err != nil {
			t.Fatalf("Validate on day %d: %v", day, err)
		}
	}

	// Past the 30-day absolute lifetime from creation, even though the
	// session was "used" only a day ago (so idle timeout alone wouldn't
	// explain a rejection here).
	clock = start.Add(31 * 24 * time.Hour)
	if _, err := sessions.Validate(ctx, token); err != auth.ErrSessionInvalid {
		t.Errorf("Validate past absolute lifetime error = %v, want %v", err, auth.ErrSessionInvalid)
	}
}

func TestSessionExpiresAfterIdleTimeout(t *testing.T) {
	queries := sqlc.New(storetest.OpenDB(t))
	user := createTestUser(t, queries)
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := start
	sessions := auth.NewSessionManagerWithClock(queries, func() time.Time { return clock })

	token, _, err := sessions.Create(ctx, user.ID, "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 8 days idle (> the 7-day idle timeout), well within the 30-day
	// absolute lifetime: idle timeout alone should reject it.
	clock = start.Add(8 * 24 * time.Hour)
	if _, err := sessions.Validate(ctx, token); err != auth.ErrSessionInvalid {
		t.Errorf("Validate past idle timeout error = %v, want %v", err, auth.ErrSessionInvalid)
	}
}
