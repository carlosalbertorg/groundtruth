package auth_test

import (
	"strings"
	"testing"
	"time"

	"github.com/carlosalbertorg/groundtruth/internal/auth"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := auth.HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if hash == "correct-horse-battery-staple" {
		t.Fatal("HashPassword returned the plaintext password unchanged")
	}

	if !auth.VerifyPassword(hash, "correct-horse-battery-staple") {
		t.Error("VerifyPassword rejected the correct password")
	}
	if auth.VerifyPassword(hash, "wrong-password") {
		t.Error("VerifyPassword accepted an incorrect password")
	}
}

func TestBurnPasswordCheckDoesRealHashingWork(t *testing.T) {
	auth.BurnPasswordCheck("warm-up: builds the lazy dummy hash")

	start := time.Now()
	auth.BurnPasswordCheck("some-password")
	elapsed := time.Since(start)

	// A real bcrypt comparison at cost 12 takes on the order of 100-300ms.
	// 20ms is far below that on any machine CI runs on, yet far above what a
	// no-op (the bug this guards against) would take.
	if elapsed < 20*time.Millisecond {
		t.Errorf("BurnPasswordCheck took %v; it should cost about as much as a real password check", elapsed)
	}
}

func TestHashPasswordRejectsPasswordsOverBcryptLimit(t *testing.T) {
	if _, err := auth.HashPassword(strings.Repeat("a", auth.MaxPasswordBytes)); err != nil {
		t.Fatalf("HashPassword rejected a password of exactly MaxPasswordBytes: %v", err)
	}
	if _, err := auth.HashPassword(strings.Repeat("a", auth.MaxPasswordBytes+1)); err == nil {
		t.Fatal("HashPassword accepted a password over MaxPasswordBytes; MaxPasswordBytes no longer matches bcrypt's limit")
	}
}

func TestHashPasswordProducesDistinctHashesForSameInput(t *testing.T) {
	a, err := auth.HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	b, err := auth.HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if a == b {
		t.Error("HashPassword produced identical hashes for the same input twice; bcrypt salting appears broken")
	}
}
