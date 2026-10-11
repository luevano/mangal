package watches

import (
	"fmt"
	"strings"
	"time"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

const timeLayout = "2006-01-02 15:04"

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Local().Format(timeLayout)
}

// short is a duration as 45s, 12m, 3h, 2d.
func short(d time.Duration) string {
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

// ago is t relative to now, in the past.
func ago(t, now time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return short(max(0, now.Sub(t))) + " ago"
}

func formatNumber(n *float32) string {
	if n == nil {
		return "-"
	}
	return stringutil.FormatFloa32(*n)
}

// nextCheck is when the watch is checked next: paused, backing off (too many failures in
// a row), due or in how long.
func nextCheck(w watch.Watch, backoff int, now time.Time) string {
	switch {
	case !w.Enabled && w.ProviderMissing:
		return "paused, provider missing"
	case !w.Enabled:
		return "paused"
	case w.ProviderMissing:
		return "provider missing"
	case !w.NextCheck.After(now):
		return "due"
	}
	next := "in " + short(w.NextCheck.Sub(now))
	if backoff > 0 && w.Failures >= backoff {
		next += " (backing off)"
	}
	return next
}

// result is what a check found: new N, nothing, error.
func result(r storage.WatchRun) string {
	switch r.Status {
	case storage.StatusNew:
		res := fmt.Sprintf("new %d", len(r.Chapters))
		if r.Error != "" {
			res += " (some failed)"
		}
		return res
	case storage.StatusNone:
		return "nothing"
	case storage.StatusError:
		return "error"
	case storage.StatusSkipped:
		return "skipped"
	}
	return r.Status
}

// chapterNumbers are the run's chapters as 65, 66, 67.5.
func chapterNumbers(r storage.WatchRun) string {
	numbers := make([]string, len(r.Chapters))
	for i, ch := range r.Chapters {
		numbers[i] = stringutil.FormatFloa32(ch.Number)
	}
	return strings.Join(numbers, ", ")
}

// runReport is a watch run in full, for the viewport.
func runReport(r storage.WatchRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n", r.Title, r.Provider)
	fmt.Fprintf(&b, "Started:  %s\n", formatTime(r.Started))
	if !r.Finished.IsZero() {
		fmt.Fprintf(&b, "Took:     %s\n", r.Finished.Sub(r.Started).Round(time.Second))
	}
	fmt.Fprintf(&b, "Status:   %s\n", result(r))
	fmt.Fprintf(&b, "From:     %s\n", formatNumber(r.From))
	if r.Error != "" {
		fmt.Fprintf(&b, "Error:    %s\n", r.Error)
	}
	if len(r.Chapters) != 0 {
		fmt.Fprintf(&b, "\n%s:\n", stringutil.Quantify(len(r.Chapters), "chapter", "chapters"))
		for _, ch := range r.Chapters {
			line := "  " + stringutil.FormatFloa32(ch.Number)
			if ch.Title != "" {
				line += " - " + ch.Title
			}
			if ch.Path != "" {
				line += "\n      " + ch.Path
			}
			b.WriteString(line + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// interval is the watch's interval, or the default's.
func interval(w watch.Watch) string {
	if w.Interval <= 0 {
		return "default (watch.interval)"
	}
	return formatInterval(time.Duration(w.Interval))
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// settings are the watch's settings and state, for its details.
func settings(w watch.Watch, backoff int, now time.Time) string {
	lines := []string{
		fmt.Sprintf("%s · %s · manga id %s", w.Provider, enabled(w), w.MangaID),
		fmt.Sprintf("interval %s · gaps %s · metadata %s · from %s", interval(w), onOff(w.Gaps), bindings(w), formatNumber(w.From)),
		fmt.Sprintf("last downloaded %s · last checked %s · next check %s", formatNumber(w.LastDownloaded), ago(w.LastChecked, now), nextCheck(w, backoff, now)),
	}
	if w.LastError != "" {
		lines = append(lines, fmt.Sprintf("%s in a row, last: %s", stringutil.Quantify(w.Failures, "failure", "failures"), w.LastError))
	}
	return strings.Join(lines, "\n")
}

func enabled(w watch.Watch) string {
	if w.Enabled {
		return "enabled"
	}
	return "paused"
}

// bindings are the watch's metadata bindings as "al 132029 (default), mal 30230".
func bindings(w watch.Watch) string {
	if len(w.Bindings) == 0 {
		return "none"
	}
	list := make([]string, len(w.Bindings))
	for i, b := range w.Bindings {
		list[i] = fmt.Sprintf("%s %s", b.Source, b.ID)
		if b.Default && len(w.Bindings) > 1 {
			list[i] += " (default)"
		}
	}
	return strings.Join(list, ", ")
}
