package watch

import (
	"fmt"
	"strings"
	"time"

	"github.com/luevano/mangal/v5/storage"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// Describe is the line of a watch's check, like the old batch script's: "Checking for new
// chapters [65-] for X from mango-mangaplus... downloaded 5 (65 → 70)".
func Describe(r Result, dryRun bool) string {
	if r.Status == storage.StatusSkipped {
		return fmt.Sprintf("%s from %s... %s", r.Title, r.Provider, outcome(r, dryRun))
	}
	what := "Downloading all chapters"
	if r.From != nil {
		what = fmt.Sprintf("Checking for new chapters [%s-]", stringutil.FormatFloat32(*r.From))
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
			numbers[i] = stringutil.FormatFloat32(ch.Number)
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
	if len(r.Skipped) > 0 {
		outcome += ", skipped " + FormatNumbers(r.Skipped) + " (skip list)"
	}
	if r.Coverage != "" {
		outcome += "; another provider? " + r.Coverage + heldBack(r.HeldBack)
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
	return stringutil.FormatFloat32(*n)
}

// FormatNext is when a check is due, for people: "due" once it's past, else how long
// until then and the wall-clock time (in now's location): "in 2h (16:00)", "in 9h
// (tomorrow 04:00)", "in 3d (Wed 04:00)", "in 12d (Oct 23 04:00)".
func FormatNext(next, now time.Time) string {
	if next.IsZero() {
		return "never"
	}
	if !next.After(now) {
		return "due"
	}
	return fmt.Sprintf("in %s (%s)", ShortDuration(next.Sub(now)), WallClock(next, now))
}

// WallClock is t as a time of day, with the day when it isn't now's: 16:00, tomorrow
// 04:00, Wed 04:00 (within a week), Oct 23 04:00, Jan 2 2027 04:00 (another year).
func WallClock(t, now time.Time) string {
	t = t.In(now.Location())
	clock := t.Format("15:04")
	y, m, d := t.Date()
	ny, nm, nd := now.Date()
	today := time.Date(ny, nm, nd, 0, 0, 0, 0, now.Location())
	days := int(time.Date(y, m, d, 0, 0, 0, 0, now.Location()).Sub(today).Round(time.Hour) / (24 * time.Hour))
	switch {
	case days == 0:
		return clock
	case days == 1:
		return "tomorrow " + clock
	case days > 1 && days < 7:
		return t.Format("Mon ") + clock
	case y == ny:
		return t.Format("Jan 2 ") + clock
	}
	return t.Format("Jan 2 2006 ") + clock
}

// ShortDuration is d in its largest unit, rounded down: 40s, 45m, 2h, 3d (from 48h).
func ShortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
}
