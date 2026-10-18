package watches

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// HoldTarget is what the hold key marks: a watch or a library manga, with its marks now
// and the mark suggested for it.
type HoldTarget struct {
	Watch, Manga int64
	Title        string
	// Hold is its hold mark, Provider true when it needs another provider.
	Hold     string
	Provider bool
	// Suggested is the mark suggested (watch.SuggestedHold, or MarkProvider for a
	// coverage), Why why, Reason the provider mark's reason.
	Suggested, Why, Reason string
}

// WatchHoldTarget is the hold target of a watch: AniList's status suggests a hold, its
// last check's coverage another provider.
func WatchHoldTarget(w watch.Watch) HoldTarget {
	t := HoldTarget{Watch: w.ID, Title: w.Title, Hold: w.Hold, Provider: w.NeedsRelink && w.RelinkKind == storage.RelinkProvider}
	switch {
	case w.Coverage != "" && !t.Provider:
		t.Suggested, t.Why, t.Reason = watch.MarkProvider, w.Coverage, w.Coverage
	case w.Hold == "":
		if t.Suggested = watch.SuggestedHold(w.Status); t.Suggested != "" {
			t.Why = library.StatusLabel(w.Status) + " on AniList"
		}
	}
	return t
}

// MangaHoldTarget is the hold target of a library manga, AniList's status suggests a hold.
func MangaHoldTarget(m storage.Manga) HoldTarget {
	t := HoldTarget{Manga: m.ID, Title: m.Title, Hold: m.Hold, Provider: m.NewProvider != ""}
	if m.Hold == "" {
		if t.Suggested = watch.SuggestedHold(m.Status); t.Suggested != "" {
			t.Why = library.StatusLabel(m.Status) + " on AniList"
		}
	}
	return t
}

// markLabel is the mark for people.
func markLabel(mark string) string {
	if mark == "" {
		return "clear the marks"
	}
	return watch.HoldLabel(mark)
}

// AskHold asks which mark to set (enter: the suggested one), pick makes the change of
// the options picked.
func AskHold(d *confirm.Dialog, behind string, t HoldTarget, pick func(watch.HoldOptions) tea.Cmd) {
	opts := func(mark string) watch.HoldOptions {
		o := watch.HoldOptions{Watch: t.Watch, Manga: t.Manga, Mark: mark}
		if mark == watch.MarkProvider {
			o.Reason = t.Reason
		}
		return o
	}
	var choices []confirm.Choice
	if t.Suggested != "" {
		choices = append(choices, confirm.Choice{Key: "enter", Label: markLabel(t.Suggested) + " (suggested)", Cmd: pick(opts(t.Suggested))})
	}
	choices = append(choices,
		confirm.Choice{Key: "h", Label: "hiatus", Cmd: pick(opts(storage.HoldHiatus))},
		confirm.Choice{Key: "e", Label: "ended", Cmd: pick(opts(storage.HoldEnded))},
		confirm.Choice{Key: "p", Label: "new provider", Cmd: pick(opts(watch.MarkProvider))},
	)
	if t.Hold != "" || t.Provider {
		choices = append(choices, confirm.Choice{Key: "c", Label: "clear", Cmd: pick(opts(""))})
	}
	message := t.Title + ": "
	switch {
	case t.Hold != "":
		message += markLabel(t.Hold)
	case t.Provider:
		message += markLabel(watch.MarkProvider)
	default:
		message += "no mark"
	}
	if t.Suggested != "" {
		message += fmt.Sprintf("\nSuggested: %s (%s)", markLabel(t.Suggested), t.Why)
	}
	message += "\n\nA hold (hiatus, ended) checks its watches once more for the last chapters, then pauses them; a check (with gaps) still gets what's missing anytime. A new provider skips it until it's re-linked."
	d.Choose("Mark", message, behind, choices...)
}

// HoldLabel is the label of the loading of a hold.
func HoldLabel(opts watch.HoldOptions, title string) string {
	switch opts.Mark {
	case storage.HoldHiatus, storage.HoldEnded:
		return "Checking " + title + " once more, then holding it"
	case "":
		return "Clearing the marks of " + title
	}
	return "Marking " + title
}

// Hold marks the target through the watches' manager, the notification says what it did.
func Hold(ctx context.Context, opts watch.HoldOptions, title string) (string, error) {
	m, err := Connect(ctx)
	if err != nil {
		return "", err
	}
	return hold(ctx, m, opts, title)
}

func hold(ctx context.Context, m watch.Manager, opts watch.HoldOptions, title string) (string, error) {
	res, err := m.Hold(ctx, opts)
	if err != nil {
		return "", err
	}
	msg := fmt.Sprintf("%s: %s", title, markLabel(opts.Mark))
	if opts.Mark == "" {
		msg = "cleared the marks of " + title
	}
	if res.Check != nil && res.Check.NewChapters > 0 {
		msg += ", got " + stringutil.Quantify(res.Check.NewChapters, "new chapter", "new chapters") + " first"
	}
	if res.CheckError != "" {
		msg += " (the check before failed: " + res.CheckError + ")"
	}
	return msg, nil
}
