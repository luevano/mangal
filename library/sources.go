package library

import (
	"strings"
	"time"

	"github.com/luevano/mangal/v5/storage"
)

// SourcesSummary is a library manga's providers, the current one first and the previous
// ones after: "mango-weebcentral (was mango-asurascans, mango-mangasee)".
func SourcesSummary(sources []storage.Source) string {
	if len(sources) == 0 {
		return ""
	}
	summary := sources[0].Provider
	var was []string
	for _, src := range sources[1:] {
		was = append(was, src.Provider)
	}
	if len(was) != 0 {
		summary += " (was " + strings.Join(was, ", ") + ")"
	}
	return summary
}

// SourceLines are a library manga's sources with their manga ids, one per line: the
// current one, then the previous ones, with when a move set each.
func SourceLines(sources []storage.Source) []string {
	lines := make([]string, len(sources))
	for i, src := range sources {
		label := "source: "
		if i > 0 {
			label = "previous: "
		}
		lines[i] = label + src.Provider + " " + src.MangaID
		if !src.Moved.IsZero() {
			lines[i] += " · moved " + src.Moved.Format(time.DateOnly)
		}
	}
	return lines
}
