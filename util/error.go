package util

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// TODO: also use the logger to possibly write to file
//
// Printf wrapper that also exits, uses stderrr.
func Errorf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format, a...)
	Exit(1)
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

// ErrorTree shows the chain of wrapped errors one layer per line, each layer with only
// what it adds: what every step was doing, down to the cause. Joined errors are branches.
func ErrorTree(err error) string {
	var sb strings.Builder
	writeErrorTree(&sb, err, "", true)
	return strings.TrimRight(sb.String(), "\n")
}

func writeErrorTree(sb *strings.Builder, err error, indent string, root bool) {
	for err != nil {
		if joined, ok := err.(interface{ Unwrap() []error }); ok {
			for _, e := range joined.Unwrap() {
				writeErrorTree(sb, e, indent, root)
			}
			return
		}

		own := err.Error()
		next := errors.Unwrap(err)
		if next != nil {
			own = strings.TrimSpace(strings.TrimSuffix(own, next.Error()))
			own = strings.TrimSpace(strings.TrimSuffix(own, ":"))
		}
		// wrappers that add nothing (e.g. "%w") don't get a line
		if own != "" {
			if root {
				sb.WriteString(own + "\n")
				root = false
			} else {
				sb.WriteString(indent + "└ " + own + "\n")
				indent += "  "
			}
		}
		err = next
	}
}
