package watch

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
)

// KindHold is the check run of a hold: the last chapters before the watches pause.
const KindHold = "hold"

// MarkProvider is the mark of a manga that needs another provider (HoldOptions.Mark).
const MarkProvider = "provider"

// Marks are the marks Hold sets: the hold marks and MarkProvider.
var Marks = append(slices.Clone(storage.HoldMarks), MarkProvider)

// ErrHeld is resuming a watch on hold: its mark has to be cleared (Hold).
var ErrHeld = errors.New("on hold")

// HoldOptions say what to mark: a watch (and the library manga it's in) or a library
// manga (and its watches).
type HoldOptions struct {
	Watch int64 `json:"watch,omitempty"`
	Manga int64 `json:"manga,omitempty"`
	// Mark is storage.HoldHiatus or HoldEnded (the manga's watches are checked once more,
	// then paused), MarkProvider (the watch, or every watch of the manga, needs a re-link
	// to another provider; a manga without one gets the mark itself), or "" to clear the
	// marks (the watches the hold paused resume).
	Mark string `json:"mark"`
	// Reason is why it needs another provider (MarkProvider), else what the watch's last
	// check found missing (its Coverage), else "marked by hand".
	Reason string `json:"reason,omitempty"`
	// NoCheck pauses without checking first.
	NoCheck bool `json:"no_check,omitempty"`
	// OnEvent is told the progress of the check, may be nil (a Remote drops it).
	OnEvent func(Event) `json:"-"`
}

// HoldRequest is the body of the API's hold of a watch or a library manga (HoldOptions
// without the target).
type HoldRequest struct {
	Mark    string `json:"mark"`
	Reason  string `json:"reason,omitempty"`
	NoCheck bool   `json:"no_check,omitempty"`
}

// HoldResult is what Hold did.
type HoldResult struct {
	// Manga is the library manga marked, 0 when the watch has none (it has the mark).
	Manga int64 `json:"manga,omitempty"`
	// Watches are the ones marked, as they are now.
	Watches []Watch `json:"watches"`
	// Check is the check before pausing, nil when there was none.
	Check *Report `json:"check,omitempty"`
	// CheckError is why the check failed: the watches were held anyway (a check of
	// each, with --gaps, gets the chapters later).
	CheckError string `json:"check_error,omitempty"`
}

// SuggestedHold is the hold mark AniList's publication status suggests: hiatus for
// HIATUS, ended for FINISHED and CANCELLED, "" otherwise.
func SuggestedHold(status string) string {
	switch metadata.Status(status) {
	case metadata.StatusHiatus:
		return storage.HoldHiatus
	case metadata.StatusFinished, metadata.StatusCancelled:
		return storage.HoldEnded
	}
	return ""
}

// Hold marks a library manga, or a watch and its library manga: on hold (hiatus, ended:
// its watches get the last chapters, then pause), as needing another provider, or
// neither (cleared). A watch on hold can still be checked when asked (with gaps too),
// never on schedule.
func (s *Service) Hold(ctx context.Context, opts HoldOptions) (HoldResult, error) {
	if opts.Mark != "" && !slices.Contains(Marks, opts.Mark) {
		return HoldResult{}, fmt.Errorf("mark %q: not one of %v", opts.Mark, Marks)
	}
	if (opts.Watch == 0) == (opts.Manga == 0) {
		return HoldResult{}, errors.New("hold: a watch or a library manga")
	}
	all, err := s.List(ctx)
	if err != nil {
		return HoldResult{}, err
	}
	var res HoldResult
	var marked []Watch
	if opts.Watch != 0 {
		i := slices.IndexFunc(all, func(w Watch) bool { return w.ID == opts.Watch })
		if i < 0 {
			return HoldResult{}, fmt.Errorf("watch %d: %w", opts.Watch, ErrNotFound)
		}
		res.Manga, marked = all[i].LibraryManga, []Watch{all[i]}
	} else {
		if _, found, err := s.Library.MangaByID(opts.Manga); err != nil {
			return HoldResult{}, err
		} else if !found {
			return HoldResult{}, fmt.Errorf("library manga %d: %w", opts.Manga, ErrNotFound)
		}
		res.Manga = opts.Manga
	}
	if res.Manga != 0 && (opts.Manga != 0 || opts.Mark != MarkProvider) {
		// a hold is the manga's, so are its watches; another provider only the watch's
		marked = slices.DeleteFunc(slices.Clone(all), func(w Watch) bool { return w.LibraryManga != res.Manga })
	}

	switch opts.Mark {
	case storage.HoldHiatus, storage.HoldEnded:
		if !opts.NoCheck && len(marked) > 0 {
			ids := make([]int64, len(marked))
			for i, w := range marked {
				ids[i] = w.ID
			}
			report, err := s.Check(ctx, ids, CheckOptions{Kind: KindHold, OnEvent: opts.OnEvent})
			switch {
			case ctx.Err() != nil:
				return HoldResult{}, ctx.Err()
			case err != nil:
				res.CheckError = err.Error()
			default:
				res.Check = &report
			}
		}
		if res.Manga != 0 {
			if err := s.Library.SetHold([]int64{res.Manga}, opts.Mark); err != nil {
				return HoldResult{}, err
			}
		}
		for _, w := range marked {
			if _, err := s.update(w.ID, func(sw *storage.Watch) {
				sw.Enabled = false
				sw.Hold = ""
				if res.Manga == 0 {
					sw.Hold = opts.Mark
				}
			}); err != nil {
				return HoldResult{}, err
			}
		}
	case MarkProvider:
		if len(marked) == 0 {
			if err := s.Library.SetNewProvider(res.Manga, cmp.Or(opts.Reason, "marked by hand")); err != nil {
				return HoldResult{}, err
			}
		}
		for _, w := range marked {
			// what its last check found, if anything
			reason := cmp.Or(opts.Reason, w.Coverage, "marked by hand")
			if _, err := s.update(w.ID, func(sw *storage.Watch) {
				sw.NeedsRelink, sw.RelinkKind, sw.RelinkReason = true, storage.RelinkProvider, reason
			}); err != nil {
				return HoldResult{}, err
			}
		}
	case "":
		if res.Manga != 0 {
			if err := s.Library.SetHold([]int64{res.Manga}, ""); err != nil {
				return HoldResult{}, err
			}
			if err := s.Library.SetNewProvider(res.Manga, ""); err != nil {
				return HoldResult{}, err
			}
		}
		for _, w := range marked {
			held := w.Hold != ""
			if _, err := s.update(w.ID, func(sw *storage.Watch) {
				sw.Hold = ""
				if sw.RelinkKind == storage.RelinkProvider {
					sw.NeedsRelink, sw.RelinkKind, sw.RelinkReason = false, "", ""
				}
				if held && !sw.Enabled {
					sw.Enabled, sw.Resumed = true, s.now()
				}
			}); err != nil {
				return HoldResult{}, err
			}
		}
	}
	for _, w := range marked {
		full, err := s.Get(ctx, w.ID)
		if err != nil {
			return HoldResult{}, err
		}
		res.Watches = append(res.Watches, full)
	}
	return res, nil
}
