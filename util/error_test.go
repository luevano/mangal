package util

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// same shape as mangodex's RateLimitError
type rateLimitError struct{}

func (rateLimitError) Error() string                     { return "429 too many requests" }
func (rateLimitError) RetryAfterDuration() time.Duration { return 30 * time.Second }

func TestRetryAfter(t *testing.T) {
	err := fmt.Errorf("download chapter: %w", fmt.Errorf("list chapters: %w", rateLimitError{}))
	if wait, ok := RetryAfter(err); !ok || wait != 30*time.Second {
		t.Errorf("wanted 30s through the wrapping, got %s, %v", wait, ok)
	}
	if _, ok := RetryAfter(errors.New("some other error")); ok {
		t.Error("other errors aren't rate limits")
	}
}
