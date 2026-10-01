package auth

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
)

// ErrSetupAlreadyComplete is returned by CreateFirstAdmin if an admin
// already exists.
var ErrSetupAlreadyComplete = errors.New("setup already complete")

// SetupGate tracks whether groundtruth's first-run setup (creating the
// initial admin account) has happened yet. Every route except the setup
// endpoints themselves is blocked until it has.
//
// Once at least one user exists, setup can never become incomplete again
// in v1 (there is no "delete all users" path), so the positive result is
// cached in-process to avoid a COUNT(*) query on every single request.
type SetupGate struct {
	queries *sqlc.Queries
	done    atomic.Bool
	mu      sync.Mutex // guards the check-then-create in CreateFirstAdmin
}

// NewSetupGate builds a SetupGate backed by queries.
func NewSetupGate(queries *sqlc.Queries) *SetupGate {
	return &SetupGate{queries: queries}
}

// Complete reports whether the first admin account has been created.
func (g *SetupGate) Complete(ctx context.Context) (bool, error) {
	if g.done.Load() {
		return true, nil
	}
	count, err := g.queries.CountUsers(ctx)
	if err != nil {
		return false, err
	}
	if count > 0 {
		g.done.Store(true)
		return true, nil
	}
	return false, nil
}

// CreateFirstAdmin atomically checks that setup isn't already complete and
// runs create — guarded by a mutex so two concurrent setup requests can't
// both pass the check and each create an "first" admin. create is only
// invoked while still holding that lock.
func (g *SetupGate) CreateFirstAdmin(ctx context.Context, create func(ctx context.Context) (sqlc.User, error)) (sqlc.User, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	done, err := g.Complete(ctx)
	if err != nil {
		return sqlc.User{}, err
	}
	if done {
		return sqlc.User{}, ErrSetupAlreadyComplete
	}

	user, err := create(ctx)
	if err != nil {
		return sqlc.User{}, err
	}
	g.done.Store(true)
	return user, nil
}
