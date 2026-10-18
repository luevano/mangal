package cmd

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/spf13/cobra"
)

var watchLogArgs = struct {
	Since    string
	Watch    int64
	Provider string
	MangaID  string
	Limit    int
	JSON     bool
	Remove   []int64
}{}

func init() {
	watchCmd.AddCommand(watchLogCmd)
	f := watchLogCmd.Flags()
	f.StringVar(&watchLogArgs.Since, "since", "7d", "How far back (e.g. 7d, 12h), 0 for everything")
	f.Int64Var(&watchLogArgs.Watch, "watch", 0, "Only the checks of this watch id")
	f.StringVar(&watchLogArgs.Provider, "provider", "", "Only the checks of this provider's mangas (removed watches too)")
	f.StringVar(&watchLogArgs.MangaID, "manga", "", "Only the checks of this manga id (with --provider)")
	_ = watchLogCmd.RegisterFlagCompletionFunc("provider", completionProviderIDs)
	f.IntVar(&watchLogArgs.Limit, "limit", 0, "Only the latest N check runs")
	f.BoolVarP(&watchLogArgs.JSON, "json", "j", false, "Print JSON")
	f.Int64SliceVar(&watchLogArgs.Remove, "rm", nil, "Delete these runs (the \"run N\" of a line) from the history")
}

var watchLogCmd = &cobra.Command{
	Use:   "log",
	Short: "Show the history of the checks",
	Long: `Show the check runs, latest first, with what each watch got: the chapters downloaded, nothing new or the error.

--rm deletes runs (one watch's check, "run N") from the history, e.g. one that recorded a
fake chapter; a check run left without runs goes too. The downloads stay. The data
database is snapshotted first: "mangal library undo" brings them back.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if len(watchLogArgs.Remove) > 0 {
			return removeRuns(cmd, watchLogArgs.Remove)
		}
		since, err := parseSince(watchLogArgs.Since)
		if err != nil {
			return err
		}
		if watchLogArgs.MangaID != "" && watchLogArgs.Provider == "" {
			return errors.New("--manga needs --provider")
		}
		svc, err := newWatchManager(cmd)
		if err != nil {
			return err
		}
		filter := watch.RunFilter{Watch: watchLogArgs.Watch, Provider: watchLogArgs.Provider, MangaID: watchLogArgs.MangaID, Limit: watchLogArgs.Limit}
		if since > 0 {
			filter.Since = time.Now().Add(-since)
		}
		runs, err := svc.Runs(cmd.Context(), filter)
		if err != nil {
			return err
		}
		if watchLogArgs.JSON {
			if runs == nil {
				runs = []watch.CheckRun{}
			}
			return printJSON(cmd.OutOrStdout(), runs)
		}
		if len(runs) == 0 {
			cmd.Println("no checks")
			return nil
		}
		for _, run := range slices.Backward(runs) {
			printRun(cmd, run)
		}
		return nil
	},
}

// removeRuns deletes the watch runs from the history.
func removeRuns(cmd *cobra.Command, ids []int64) error {
	svc, err := newWatchManager(cmd)
	if err != nil {
		return err
	}
	var all []watch.RemovedRun
	if local, ok := svc.(*watch.Service); ok {
		// one operation, one snapshot
		all, err = local.RemoveRuns(cmd.Context(), ids)
		snapshot := ""
		if len(all) != 0 {
			snapshot = all[0].Snapshot
		}
		undoHint(cmd, snapshot, err)
	} else {
		for _, id := range ids {
			removed, rmErr := svc.RemoveRun(cmd.Context(), id)
			undoHint(cmd, removed.Snapshot, rmErr)
			if err = rmErr; err != nil {
				break
			}
			all = append(all, removed)
		}
	}
	for _, removed := range all {
		r := removed.Run
		successf(cmd, "deleted run %d (%s from %s, %s check %d, %s) from the history", r.ID, r.Title, r.Provider, r.Status, r.CheckRun, watch.FormatTime(r.Started))
		if removed.CheckRunRemoved {
			cmd.Printf("check %d had no other run, deleted too\n", r.CheckRun)
		}
	}
	return err
}

func printRun(cmd *cobra.Command, run watch.CheckRun) {
	took := "unfinished"
	if !run.Finished.IsZero() {
		took = run.Finished.Sub(run.Started).Round(time.Second).String()
	}
	cmd.Printf("# %s check %d, %s (%s): %s, %s, %s\n", run.Kind, run.ID, watch.FormatTime(run.Started), took,
		stringutil.Quantify(run.Watches, "watch", "watches"), stringutil.Quantify(run.NewChapters, "new chapter", "new chapters"),
		stringutil.Quantify(run.Errors, "error", "errors"))
	for _, r := range run.Results {
		from := "all"
		if r.From != nil {
			from = stringutil.FormatFloat32(*r.From) + "-"
		}
		line := fmt.Sprintf("  run %d  %s (%s) [%s]: ", r.ID, r.Title, r.Provider, from)
		switch r.Status {
		case storage.StatusNew:
			line += fmt.Sprintf("%d new", r.New())
		case storage.StatusNone:
			line += "no new chapters"
		default:
			line += r.Status
		}
		if r.Error != "" {
			line += ": " + r.Error
		}
		if len(r.Skipped) > 0 {
			line += ", skipped " + watch.FormatNumbers(r.Skipped) + " (skip list)"
		}
		cmd.Println(line)
		for _, ch := range r.Chapters {
			cmd.Printf("    %s  %s\n", stringutil.FormatFloat32(ch.Number), ch.Path)
		}
	}
}

var watchStaleArgs = struct {
	Since string
	JSON  bool
}{}

func init() {
	watchCmd.AddCommand(watchStaleCmd)
	watchStaleCmd.Flags().StringVar(&watchStaleArgs.Since, "since", "90d", "No new chapter for this long (e.g. 90d)")
	watchStaleCmd.Flags().BoolVarP(&watchStaleArgs.JSON, "json", "j", false, "Print JSON")
}

var watchStaleCmd = &cobra.Command{
	Use:   "stale",
	Short: "List the watches without new chapters for a while",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		since, err := parseSince(watchStaleArgs.Since)
		if err != nil {
			return err
		}
		svc, err := newWatchManager(cmd)
		if err != nil {
			return err
		}
		stale, err := svc.Stale(cmd.Context(), since)
		if err != nil {
			return err
		}
		if watchStaleArgs.JSON {
			if stale == nil {
				stale = []watch.Watch{}
			}
			return printJSON(cmd.OutOrStdout(), stale)
		}
		if len(stale) == 0 {
			cmd.Printf("every watch had a new chapter in the last %s\n", watchStaleArgs.Since)
			return nil
		}
		for _, w := range stale {
			last := "no new chapter since added " + watch.FormatTime(w.Added)
			if !w.LastNewAt.IsZero() {
				days := int(time.Since(w.LastNewAt).Hours() / 24)
				last = fmt.Sprintf("last new chapter %s on %s (%s ago)", watch.FormatNumber(w.LastNew), watch.FormatTime(w.LastNewAt), stringutil.Quantify(days, "day", "days"))
			}
			cmd.Printf("%d  %s (%s): %s\n", w.ID, w.Title, w.Provider, last)
		}
		return nil
	},
}
