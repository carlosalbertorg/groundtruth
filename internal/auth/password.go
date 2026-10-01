// Package auth implements groundtruth's dashboard authentication:
// password hashing, opaque database-backed sessions, and the HTTP
// middleware that enforces them.
package auth

import "golang.org/x/crypto/bcrypt"

// bcryptCost is deliberately above bcrypt's own default (10): groundtruth
// reaches real cloud credentials via the workspaces it manages, so the
// dashboard's own login is worth the extra hashing cost.
const bcryptCost = 12

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
