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

func TestErrorTree(t *testing.T) {
	cause := errors.New("mangodex: GET /at-home/server/x: 429 too many requests")
	err := fmt.Errorf("download chapter %q: %w", "Chapter 3", fmt.Errorf("page 4: %w", fmt.Errorf("mango-mangadex: image for chapter %q: %w", "Chapter 3", cause)))
	want := `download chapter "Chapter 3"
└ page 4
  └ mango-mangadex: image for chapter "Chapter 3"
    └ mangodex: GET /at-home/server/x: 429 too many requests`
	if got := ErrorTree(err); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}

	if got := ErrorTree(errors.New("plain")); got != "plain" {
		t.Errorf("got %q", got)
	}

	joined := fmt.Errorf("search metadata: %w", errors.Join(errors.New("anilist: down"), errors.New("mal: down")))
	if got, want := ErrorTree(joined), "search metadata\n└ anilist: down\n└ mal: down"; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
