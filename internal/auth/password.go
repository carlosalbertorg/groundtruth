// Package auth implements groundtruth's dashboard authentication:
// password hashing, opaque database-backed sessions, and the HTTP
// middleware that enforces them.
package auth

import (
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is deliberately above bcrypt's own default (10): groundtruth
// reaches real cloud credentials via the workspaces it manages, so the
// dashboard's own login is worth the extra hashing cost.
const bcryptCost = 12

// MaxPasswordBytes is bcrypt's hard input limit. golang.org/x/crypto/bcrypt
// refuses anything longer rather than silently truncating it, so callers
// that accept a new password must reject longer ones up front.
const MaxPasswordBytes = 72

// HashPassword returns a bcrypt hash of password, suitable for storing in
// users.password_hash.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword reports whether password matches the given bcrypt hash.
func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

var (
	dummyHashOnce sync.Once
	dummyHash     []byte
)

// BurnPasswordCheck spends the time a real password verification would,
// for a login attempt that has no account behind it. Without it, "no such
// email" returns in about a millisecond while "wrong password" takes a
// full bcrypt comparison, so response time alone would reveal which email
// addresses have accounts - even though both answer with the same body.
//
// The throwaway hash is built lazily, once, at the real cost, so neither
// process startup nor the (common) all-accounts-exist path pays for it.
func BurnPasswordCheck(password string) {
	dummyHashOnce.Do(func() {
		// Only fails for a cost out of range or a password over 72 bytes;
		// neither applies to this constant input.
		dummyHash, _ = bcrypt.GenerateFromPassword([]byte("groundtruth-dummy-password"), bcryptCost)
	})
	_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
}
