package util

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// TODO: also use the logger to possibly write to file
//
// Printf wrapper that also exits, uses stderrr.
func Errorf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format, a...)
	os.Exit(1)
}

// RateLimited is implemented by provider errors that say how long to wait,
// e.g. mangodex's RateLimitError.
type RateLimited interface {
	RetryAfterDuration() time.Duration
}

// RetryAfter returns how long to wait if err (or one it wraps) is RateLimited.
func RetryAfter(err error) (time.Duration, bool) {
	var rateLimit RateLimited
	if errors.As(err, &rateLimit) {
		return rateLimit.RetryAfterDuration(), true
	}
	return 0, false
}
