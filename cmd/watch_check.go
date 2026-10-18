package cmd

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/spf13/cobra"
)

var watchCheckArgs = struct {
	DryRun   bool
	Gaps     bool
	JSON     bool
	Provider string
	Due      bool
}{}

func init() {
	watchCmd.AddCommand(watchCheckCmd)
	f := watchCheckCmd.Flags()
	f.BoolVarP(&watchCheckArgs.DryRun, "dry-run", "n", false, "Only list what would be downloaded")
	f.BoolVar(&watchCheckArgs.Gaps, "gaps", false, "Also download the missing chapters below the last downloaded one, for every watch checked")
	f.BoolVarP(&watchCheckArgs.JSON, "json", "j", false, "Print the report as JSON at the end (instead of the lines)")
	f.StringVar(&watchCheckArgs.Provider, "provider", "", "Every enabled watch of this provider")
	f.BoolVar(&watchCheckArgs.Due, "due", false, "Only the watches that are due")
	_ = watchCheckCmd.RegisterFlagCompletionFunc("provider", completionProviderIDs)
}

var watchCheckCmd = &cobra.Command{
	Use:   "check [id...] | --provider <id> [--due]",
	Short: "Check the watches now",
	Long: `Check the watches with the ids, or every one that isn't paused or waiting for a
re-link (--provider: of that provider; --due: only the due ones), and download what's new.
A check that doesn't find the manga marks the watch as needing a re-link (mangal watch
move), one that finds it clears that. A check counts as the
scheduled one: the next check is the schedule's next tick.

A provider is checked by one check at a time, also across processes (e.g. "mangal
serve"): the watches of a provider another check or a download has are skipped, and when
every one is busy it's an error. Errors are sent to Discord as configured (watch.notify), and make the
exit code 1.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if watchCheckArgs.Provider != "" && len(args) > 0 {
			return errors.New("ids or --provider, not both")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		ids, err := parseIDs(args)
		if err != nil {
			return err
		}
		svc, err := newWatchManager(cmd)
		if err != nil {
			return err
		}
		if watchCheckArgs.Provider != "" || watchCheckArgs.Due {
			all, err := svc.List(cmd.Context())
			if err != nil {
				return err
			}
			ids = selectWatches(all, ids, watchCheckArgs.Provider, watchCheckArgs.Due, time.Now())
			if len(ids) == 0 {
				cmd.Println("no watches to check")
				return nil
			}
		}
		kind := watch.KindManual
		if watchCheckArgs.Gaps {
			kind = watch.KindGaps
		}
		opts := watch.CheckOptions{Kind: kind, DryRun: watchCheckArgs.DryRun, Gaps: watchCheckArgs.Gaps}
		st := newStatus(cmd)
		defer st.Done()
		opts.OnLive = func(e watch.LiveEvent) { printLiveEvent(cmd, st, e, watchCheckArgs.JSON) }
		if !watchCheckArgs.JSON {
			opts.OnEvent = func(e watch.Event) { st.Print(func() { printCheckEvent(cmd, e, watchCheckArgs.DryRun) }) }
		}
		report, err := svc.Check(cmd.Context(), ids, opts)
		st.Done()
		if err != nil {
			return err
		}
		_, remote := svc.(*watch.Remote)
		switch {
		case watchCheckArgs.JSON:
			if err := printJSON(cmd.OutOrStdout(), report); err != nil {
				return err
			}
		case len(report.Results) == 0:
			cmd.Println("no watches to check")
		case remote:
			// the progress streamed, the results are what the server recorded
			for _, r := range report.Results {
				printResult(cmd, r, watchCheckArgs.DryRun)
			}
		}
		for _, p := range report.Busy {
			cmd.PrintErrf("%s is busy (another check or a download), its watches were skipped\n", p)
		}
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		if report.Errors > 0 {
			return fmt.Errorf("%d of %d watches failed", report.Errors, report.Watches)
		}
		return nil
	},
}

// selectWatches are the watches to check: the enabled ones of the provider (or the ones
// with the ids, or every enabled one), only the due ones with due.
func selectWatches(all []watch.Watch, ids []int64, provider string, due bool, now time.Time) []int64 {
	var selected []int64
	for _, w := range all {
		switch {
		case len(ids) > 0 && !slices.Contains(ids, w.ID):
		case len(ids) == 0 && !w.Scheduled():
		case provider != "" && w.Provider != provider:
		case due && w.NextCheck.After(now):
		default:
			selected = append(selected, w.ID)
		}
	}
	return selected
}

func printResult(cmd *cobra.Command, r watch.Result, dryRun bool) {
	cmd.Println(watch.Describe(r, dryRun))
	if !dryRun {
		for _, ch := range r.Chapters {
			cmd.Printf("  %s\n", ch.Path)
		}
	}
}

// printCheckEvent prints each watch once checked (providers run in parallel, the lines
// would mix otherwise).
func printCheckEvent(cmd *cobra.Command, e watch.Event, dryRun bool) {
	if e.Kind == watch.EventDone {
		printResult(cmd, *e.Result, dryRun)
	}
}

// printLiveEvent prints the progress of a check on stderr, each line with its manga: the
// chapters found and downloaded, the waits and what the providers say; the routine steps
// (pages) are only the status line. With --json only the status line.
func printLiveEvent(cmd *cobra.Command, st *status, e watch.LiveEvent, quiet bool) {
	switch e.Kind {
	case watch.LiveStart:
		st.Transient(e.Message)
	case watch.LiveChapter, watch.LiveProgress:
		st.Transient(e.Line())
	case watch.LiveFound, watch.LiveWait, watch.LiveStatus, watch.LiveChapterDone:
		if quiet {
			st.Transient(e.Line())
			return
		}
		st.Print(func() { cmd.PrintErrln(e.Line()) })
	}
}
