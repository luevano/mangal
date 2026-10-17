package cmd

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/library"
	"github.com/spf13/cobra"
)

func init() {
	watchCmd.AddCommand(holdCmd(false))
	libraryCmd.AddCommand(holdCmd(true))
}

const holdLong = `Marks:
  hiatus    on hiatus (no chapters for a long time, it may come back)
  ended     no longer releasing (finished, cancelled, dropped)
  provider  needs another provider (it lost its chapters, or only lists the latest)

hiatus and ended are the library manga's (a watch without one gets its own): its watches
are checked once more for the last chapters (--no-check skips it), then paused, never
checked on schedule until the mark is cleared ("mangal watch check <id> --gaps" still
downloads what's missing anytime). "mangal health" suggests them from AniList (W14).

provider marks the watch as needing a re-link to another provider ("needs a new
provider", skipped by the scheduler until "mangal watch move <id> <provider>" or "mangal
library move"); a library manga without a watch gets the mark itself. "mangal health"
suggests it from the checks (W15).

--clear clears the marks: the watches the hold paused resume.`

func holdCmd(manga bool) *cobra.Command {
	var clear, noCheck bool
	var reason string
	what, example := "watch", "mangal watch hold 12 hiatus\n  mangal watch hold 12 provider --reason \"only has 60-63\"\n  mangal watch hold 12 --clear"
	if manga {
		what, example = "library manga", "mangal library hold 40 ended\n  mangal library hold 40 41 hiatus --no-check\n  mangal library hold 40 --clear"
	}
	c := &cobra.Command{
		Use:     "hold <id>... <hiatus|ended|provider> | <id>... --clear",
		Short:   fmt.Sprintf("Mark a %s on hold (hiatus, ended) or as needing another provider", what),
		Long:    fmt.Sprintf("Mark %ss (ids from \"mangal %s ls\").\n\n%s", what, strings.Fields(what)[0], holdLong),
		Example: "  " + example,
		Args: func(cmd *cobra.Command, args []string) error {
			if clear {
				return cobra.MinimumNArgs(1)(cmd, args)
			}
			return cobra.MinimumNArgs(2)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			mark := ""
			if !clear {
				mark, args = args[len(args)-1], args[:len(args)-1]
				if !isMark(mark) {
					return fmt.Errorf("%q isn't a mark (%s), or --clear", mark, strings.Join(watch.Marks, ", "))
				}
			}
			if reason != "" && mark != watch.MarkProvider {
				return errors.New("--reason is for the provider mark")
			}
			ids, err := parseIDs(args)
			if err != nil {
				return err
			}
			svc, err := newWatchManager(cmd)
			if err != nil {
				return err
			}
			st := newStatus(cmd)
			defer st.Done()
			for _, id := range ids {
				opts := watch.HoldOptions{Watch: id, Mark: mark, Reason: reason, NoCheck: noCheck}
				if manga {
					opts = watch.HoldOptions{Manga: id, Mark: mark, Reason: reason, NoCheck: noCheck}
				}
				opts.OnEvent = func(e watch.Event) { st.Print(func() { printCheckEvent(cmd, e, false) }) }
				res, err := svc.Hold(cmd.Context(), opts)
				st.Done()
				if err != nil {
					return err
				}
				_, remote := svc.(*watch.Remote)
				printHold(cmd, id, manga, mark, remote, res)
			}
			return nil
		},
	}
	f := c.Flags()
	f.BoolVar(&clear, "clear", false, "Clear the marks (the watches the hold paused resume)")
	f.BoolVar(&noCheck, "no-check", false, "Pause without checking for the last chapters first")
	f.StringVar(&reason, "reason", "", "Why it needs another provider (provider mark)")
	return c
}

func isMark(s string) bool { return slices.Contains(watch.Marks, s) }

func printHold(cmd *cobra.Command, id int64, manga bool, mark string, remote bool, res watch.HoldResult) {
	if remote && res.Check != nil {
		// the server checked: say what it got
		for _, r := range res.Check.Results {
			printResult(cmd, r, false)
		}
	}
	if res.CheckError != "" {
		cmd.PrintErrf("warning: the check before the hold failed (%s): \"mangal watch check <id> --gaps\" gets the chapters later\n", res.CheckError)
	}
	subject := fmt.Sprintf("watch %d", id)
	if manga || res.Manga != 0 && mark != watch.MarkProvider {
		subject = fmt.Sprintf("library manga %d", res.Manga)
	}
	switch mark {
	case "":
		successf(cmd, "cleared the marks of %s", subject)
	case watch.MarkProvider:
		successf(cmd, "marked %s as needing a new provider", subject)
	default:
		successf(cmd, "%s is %s", subject, library.HoldLabel(mark))
	}
	for _, w := range res.Watches {
		state := "paused"
		switch {
		case w.NeedsRelink:
			state = w.RelinkLabel()
		case w.Enabled:
			state = "resumed"
		}
		cmd.Printf("  watch %d %q from %s: %s\n", w.ID, w.Title, w.Provider, state)
	}
}
