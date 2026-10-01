package terraform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadCredentialEnvFileParseErrorNeverLeaksFileContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "creds.env")
	// An unterminated quoted value - exactly the godotenv parse error
	// shape that otherwise embeds the raw source line (including this
	// secret-looking value) directly into its error message.
	const secretLookingValue = "AKIA-super-secret-value-that-must-never-leak"
	if err := os.WriteFile(path, []byte(`AWS_SECRET_ACCESS_KEY="`+secretLookingValue), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := readCredentialEnvFile(path)
	if err == nil {
		t.Fatal("expected a parse error for an unterminated quoted value")
	}
	if strings.Contains(err.Error(), secretLookingValue) {
		t.Fatalf("error leaked file content: %v", err)
	}
	if err != errCredentialFileParse {
		t.Errorf("error = %v, want the generic errCredentialFileParse", err)
	}
}

func TestReadCredentialEnvFileMissingFileReturnsPlainOSError(t *testing.T) {
	_, err := readCredentialEnvFile(filepath.Join(t.TempDir(), "does-not-exist.env"))
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}
	if !os.IsNotExist(err) {
		t.Errorf("error = %v, want an os.IsNotExist error", err)
	}
}

func TestReadCredentialEnvFileParsesValidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "creds.env")
	if err := os.WriteFile(path, []byte("FOO=bar\nBAZ=qux\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	env, err := readCredentialEnvFile(path)
	if err != nil {
		t.Fatalf("readCredentialEnvFile: %v", err)
	}
	if env["FOO"] != "bar" || env["BAZ"] != "qux" {
		t.Errorf("env = %v, want FOO=bar BAZ=qux", env)
	}
}
