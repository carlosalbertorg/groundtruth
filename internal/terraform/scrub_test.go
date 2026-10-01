package terraform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestScrubSecretsReplacesEveryOccurrence(t *testing.T) {
	creds := map[string]string{"TOKEN": "abcd-1234-efgh"}

	got := scrubSecrets("401 for abcd-1234-efgh; retry with abcd-1234-efgh failed", creds)
	want := "401 for [redacted]; retry with [redacted] failed"
	if got != want {
		t.Errorf("scrubSecrets = %q, want %q", got, want)
	}
}

func TestScrubSecretsHandlesOverlappingValuesLongestFirst(t *testing.T) {
	// "secret" is a substring of "secret-suffix". Replacing the short one
	// first would leave "[redacted]-suffix" behind, exposing the tail of the
	// longer value.
	creds := map[string]string{"SHORT": "secret", "LONG": "secret-suffix"}

	got := scrubSecrets("saw secret-suffix and secret", creds)
	if strings.Contains(got, "suffix") {
		t.Errorf("scrubSecrets = %q, leaked the tail of the longer value", got)
	}
}

func TestScrubSecretsLeavesVeryShortValuesAlone(t *testing.T) {
	creds := map[string]string{"FLAG": "1", "MODE": "on"}

	text := "retry 1 of 3, mode on"
	if got := scrubSecrets(text, creds); got != text {
		t.Errorf("scrubSecrets = %q, want the text unchanged (values too short to be worth scrubbing)", got)
	}
}

func TestScrubErrorKeepsErrorsIsButNotUnwrap(t *testing.T) {
	creds := map[string]string{"KEY": "super-secret-value"}
	original := fmt.Errorf("plan failed: %w: using super-secret-value", context.DeadlineExceeded)

	err := scrubError(original, creds)

	if strings.Contains(err.Error(), "super-secret-value") {
		t.Errorf("error text still contains the secret: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("errors.Is no longer recognizes the wrapped deadline error")
	}
	if errors.Unwrap(err) != nil {
		t.Error("scrubbed error unwraps to the original, whose text still contains the secret")
	}
}

func TestScrubErrorReturnsOriginalWhenNothingToScrub(t *testing.T) {
	original := errors.New("plan failed")

	if got := scrubError(original, map[string]string{"KEY": "unrelated-value"}); got != original {
		t.Error("scrubError wrapped an error whose text contained no secrets")
	}
	if got := scrubError(original, nil); got != original {
		t.Error("scrubError wrapped an error when there were no credentials at all")
	}
	if got := scrubError(nil, map[string]string{"KEY": "some-value"}); got != nil {
		t.Errorf("scrubError(nil) = %v, want nil", got)
	}
}
