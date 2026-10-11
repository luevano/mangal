package util

import (
	"errors"
	"fmt"
	"testing"
)

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
