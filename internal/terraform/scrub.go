package terraform

import (
	"errors"
	"sort"
	"strings"
)

const (
	// scrubPlaceholder replaces any credential value found in error text.
	scrubPlaceholder = "[redacted]"

	// minScrubLength is the shortest credential value that gets scrubbed.
	// Replacing a one- to three-character value ("1", "on") everywhere in
	// an error message would garble it without protecting anything: a
	// secret that short has no entropy to protect.
	minScrubLength = 4
)

// scrubSecrets returns text with every value in creds replaced by
// scrubPlaceholder.
//
// groundtruth knows exactly which credential values it injected into the
// terraform/tofu subprocess, and that subprocess's stderr ends up in a
// check's stored error_message (and from there in the API and the UI). A
// provider or backend can echo a value it was given - an API key in an
// HTTP error, a token in a rejected URL - so removing the known values is
// the one defense that doesn't depend on every provider behaving.
//
// It deliberately over-redacts: a credential file's non-secret values (a
// region, an endpoint) are replaced too, because telling them apart from
// secrets by name would be a guess, and a wrong guess leaks a secret.
func scrubSecrets(text string, creds map[string]string) string {
	values := make([]string, 0, len(creds))
	for _, v := range creds {
		if len(v) >= minScrubLength {
			values = append(values, v)
		}
	}
	// Longest first, so a value that happens to be a substring of another
	// can't be replaced first and leave the rest of the longer one exposed.
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })

	for _, v := range values {
		text = strings.ReplaceAll(text, v, scrubPlaceholder)
	}
	return text
}

// scrubbedError is an error whose message has had credential values
// removed. It matches the original under errors.Is (so a deadline or
// cancellation is still recognizable) but deliberately has no Unwrap: that
// would let a caller walk back to the original error and print the
// unscrubbed text.
type scrubbedError struct {
	msg string
	err error
}

func (e *scrubbedError) Error() string { return e.msg }

func (e *scrubbedError) Is(target error) bool { return errors.Is(e.err, target) }

// scrubError returns err unchanged when there is nothing to scrub, and a
// scrubbedError otherwise.
func scrubError(err error, creds map[string]string) error {
	if err == nil || len(creds) == 0 {
		return err
	}
	msg := err.Error()
	scrubbed := scrubSecrets(msg, creds)
	if scrubbed == msg {
		return err
	}
	return &scrubbedError{msg: scrubbed, err: err}
}
