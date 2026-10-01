package terraform

import (
	"errors"
	"os"

	"github.com/joho/godotenv"
)

// errCredentialFileParse deliberately carries no detail: godotenv's own
// parse errors can embed the offending source line verbatim (e.g.
// `fmt.Errorf("unterminated quoted value %s", src[...])`) - and a line
// from a *credentials* file is exactly the one piece of text that must
// never reach a log, an HTTP response, or a stored error_message. Losing
// godotenv's error detail here is an intentional, one-way trade.
var errCredentialFileParse = errors.New("parse credential env file: invalid syntax")

// readCredentialEnvFile reads a dotenv-format file fresh from disk on
// every call. groundtruth never caches, logs, or persists its contents -
// it only ever lives in-memory for the duration of one check, merged
// into that one subprocess's environment. See docs/ARCHITECTURE.md for
// the full rationale behind not storing cloud credentials at all.
func readCredentialEnvFile(path string) (map[string]string, error) {
	// Checked separately, before handing the file to godotenv: a "can't
	// open this path" error (not found, permission denied, ...) is just
	// a path and an OS error code - safe to return as-is. Only a parse
	// failure risks echoing real file content (see errCredentialFileParse).
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}

	env, err := godotenv.Read(path)
	if err != nil {
		return nil, errCredentialFileParse
	}
	return env, nil
}
