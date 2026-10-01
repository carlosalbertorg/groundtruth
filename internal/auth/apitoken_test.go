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

func TestAPITokenCreateAndValidate(t *testing.T) {
	queries := sqlc.New(storetest.OpenDB(t))
	user := createTestUser(t, queries)
	tokens := auth.NewAPITokenManager(queries)
	ctx := context.Background()

	token, err := tokens.Create(ctx, user.ID, uuid.NewString(), "ci-token")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if token == "" {
		t.Fatal("Create returned an empty token")
	}

	got, err := tokens.Validate(ctx, token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got.UserID != user.ID {
		t.Errorf("UserID = %q, want %q", got.UserID, user.ID)
	}
	if !got.LastUsedAt.Valid {
		t.Error("expected last_used_at to be set after Validate")
	}
}

func TestAPITokenValidateRejectsUnknownToken(t *testing.T) {
	queries := sqlc.New(storetest.OpenDB(t))
	tokens := auth.NewAPITokenManager(queries)

	if _, err := tokens.Validate(context.Background(), "gt_this-does-not-exist"); err != auth.ErrSessionInvalid {
		t.Errorf("Validate error = %v, want %v", err, auth.ErrSessionInvalid)
	}
}

func TestAPITokenValidateRejectsRevokedToken(t *testing.T) {
	queries := sqlc.New(storetest.OpenDB(t))
	user := createTestUser(t, queries)
	tokens := auth.NewAPITokenManager(queries)
	ctx := context.Background()

	id := uuid.NewString()
	token, err := tokens.Create(ctx, user.ID, id, "ci-token")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := tokens.Revoke(ctx, user.ID, id); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if _, err := tokens.Validate(ctx, token); err != auth.ErrSessionInvalid {
		t.Errorf("Validate after Revoke error = %v, want %v", err, auth.ErrSessionInvalid)
	}
}

func TestAPITokenRevokeIsScopedToOwner(t *testing.T) {
	queries := sqlc.New(storetest.OpenDB(t))
	owner := createTestUser(t, queries)
	intruder, err := queries.CreateUser(context.Background(), sqlc.CreateUserParams{
		ID: uuid.NewString(), Email: "intruder@example.com", PasswordHash: "x", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("create intruder user: %v", err)
	}

	tokens := auth.NewAPITokenManager(queries)
	ctx := context.Background()
	id := uuid.NewString()
	token, err := tokens.Create(ctx, owner.ID, id, "owner-token")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Revoking as a different user must be a no-op.
	if err := tokens.Revoke(ctx, intruder.ID, id); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if _, err := tokens.Validate(ctx, token); err != nil {
		t.Errorf("token should still be valid after a revoke attempt by a different user, got: %v", err)
	}
}
