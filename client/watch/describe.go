package watch

import (
	"fmt"
	"strings"

	"github.com/luevano/mangal/v5/storage"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// Describe is the line of a watch's check, like the old batch script's: "Checking for new
// chapters [65-] for X from mango-mangaplus... downloaded 5 (65 → 70)".
func Describe(r Result, dryRun bool) string {
	what := "Downloading all chapters"
	if r.From != nil {
		what = fmt.Sprintf("Checking for new chapters [%s-]", stringutil.FormatFloa32(*r.From))
	}
	if r.Gaps {
		what += " and gaps"
	}
	return fmt.Sprintf("%s for %s from %s... %s", what, r.Title, r.Provider, outcome(r, dryRun))
}

func outcome(r Result, dryRun bool) string {
	n := len(r.Chapters)
	var outcome string
	switch {
	case r.Status == storage.StatusSkipped:
		return "skipped (" + r.Error + ")"
	case n == 0 && r.Error != "":
		return "ERROR " + r.Error
	case n == 0:
		outcome = "no new chapters"
	case dryRun:
		numbers := make([]string, n)
		for i, ch := range r.Chapters {
			numbers[i] = stringutil.FormatFloa32(ch.Number)
		}
		outcome = fmt.Sprintf("would download %d (%s)", n, strings.Join(numbers, ", "))
	case r.From != nil && *r.Newest() < *r.From:
		// only gaps below the last one
		outcome = fmt.Sprintf("downloaded %d (%s → %s)", n, formatNumber(r.Lowest()), formatNumber(r.Newest()))
	default:
		outcome = fmt.Sprintf("downloaded %d (%s → %s)", n, formatNumber(r.From), formatNumber(r.Newest()))
	}
	if r.Unavailable > 0 {
		outcome += fmt.Sprintf(", %d unavailable", r.Unavailable)
	}
	if r.Error != "" {
		outcome += ", ERROR " + r.Error
	}
	return outcome
}

func formatNumber(n *float32) string {
	if n == nil {
		return "-"
	}
	return stringutil.FormatFloa32(*n)
}
