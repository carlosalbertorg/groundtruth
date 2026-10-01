package auth_test

import (
	"testing"

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
