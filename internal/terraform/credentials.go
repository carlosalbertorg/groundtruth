package terraform

import "github.com/joho/godotenv"

// readCredentialEnvFile reads a dotenv-format file fresh from disk on
// every call. groundtruth never caches, logs, or persists its contents -
// it only ever lives in-memory for the duration of one check, merged
// into that one subprocess's environment. See docs/ARCHITECTURE.md for
// the full rationale behind not storing cloud credentials at all.
func readCredentialEnvFile(path string) (map[string]string, error) {
	return godotenv.Read(path)
}
