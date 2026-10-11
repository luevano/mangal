package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/notify"
	"github.com/luevano/mangal/v5/storage"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(watchCmd)
}

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Keep mangas up to date",
	Long: `A watch is one provider's manga mangal keeps up to date: each check downloads the
chapters from the last one the library has up (a .5 published after it too), and with gap
filling the missing ones below it. "mangal watch check" checks them once (e.g. from a
timer), "mangal serve" on a schedule (watch.interval).`,
	Args: cobra.NoArgs,
}

// newWatchManager is the running `mangal serve` of this config, else the local watch
// service with the Discord notifications.
func newWatchManager(cmd *cobra.Command) (watch.Manager, error) {
	m, err := watch.Connect(cmd.Context(), notify.Watches())
	if err != nil {
		return nil, err
	}
	if svc, ok := m.(*watch.Service); ok {
		svc.OnError = func(err error) {
			log.Warn("%s", err)
			cmd.PrintErrf("warning: %s\n", err)
		}
	}
	return m, nil
}

// parseID is a watch id argument.
func parseID(arg string) (int64, error) {
	id, err := strconv.ParseInt(arg, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%q isn't a watch id (see mangal watch ls)", arg)
	}
	return id, nil
}

func parseIDs(args []string) ([]int64, error) {
	ids := make([]int64, 0, len(args))
	for _, arg := range args {
		id, err := parseID(arg)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// parseSince is a duration that also takes days ("7d", "1.5d").
func parseSince(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.ParseFloat(days, 64)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("%q isn't a number of days", s)
		}
		return time.Duration(n * float64(24*time.Hour)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("%q isn't a duration (e.g. 7d, 12h)", s)
	}
	return d, nil
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

const timeLayout = "2006-01-02 15:04"

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Local().Format(timeLayout)
}

func formatLast(n *float32) string {
	if n == nil {
		return "-"
	}
	return stringutil.FormatFloa32(*n)
}

var watchAddArgs = struct {
	Selector  string
	AnilistID int
	From      float32
	Gaps      bool
	Interval  time.Duration
}{}

func init() {
	watchCmd.AddCommand(watchAddCmd)
	f := watchAddCmd.Flags()
	f.StringVarP(&watchAddArgs.Selector, "manga-selector", "m", "exact", "Manga selector of the search (first|last|exact|closest|<index>)")
	f.IntVarP(&watchAddArgs.AnilistID, "anilist-id", "a", 0, "AniList manga to bind (its metadata is used for the downloads)")
	f.Float32Var(&watchAddArgs.From, "from", 0, "Treat the chapters up to this number as downloaded (read elsewhere)")
	f.BoolVar(&watchAddArgs.Gaps, "gaps", false, "Also download the missing chapters below the last downloaded one (default: config watch.gaps)")
	f.DurationVar(&watchAddArgs.Interval, "interval", 0, "Check interval of this manga (default: config watch.interval)")
}

var watchAddCmd = &cobra.Command{
	Use:   "add <provider> <query|mid:id>",
	Short: "Watch a manga",
	Long: `Watch a manga of the provider: the one the manga selector picks from a search of the
query (exact title by default), or the manga with the id ("mid:<id>", as "mangal inline
json" shows it). Its first check downloads every chapter the library doesn't have, or the
ones after --from.`,
	Example: `  mangal watch add mango-mangapill "One-Punch Man" --from 200
  mangal watch add mango-mangaplus mid:100171 --anilist-id 132029`,
	Args:              cobra.MinimumNArgs(2),
	ValidArgsFunction: completionProviderIDs,
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newWatchManager(cmd)
		if err != nil {
			return err
		}
		opts := watch.AddOptions{
			Provider:  args[0],
			Query:     strings.Join(args[1:], " "),
			Selector:  watchAddArgs.Selector,
			AnilistID: watchAddArgs.AnilistID,
			Interval:  watchAddArgs.Interval,
		}
		if cmd.Flags().Changed("from") {
			opts.From = &watchAddArgs.From
		}
		if cmd.Flags().Changed("gaps") {
			opts.Gaps = &watchAddArgs.Gaps
		}
		w, err := svc.Add(cmd.Context(), opts)
		if err != nil {
			return err
		}
		successf(cmd, "watching %q from %s (id %d), last downloaded %s", w.Title, w.Provider, w.ID, formatLast(w.LastDownloaded))
		return nil
	},
}

var watchLsArgs = struct {
	JSON bool
}{}

func init() {
	watchCmd.AddCommand(watchLsCmd)
	watchLsCmd.Flags().BoolVar(&watchLsArgs.JSON, "json", false, "Print JSON")
}

var watchLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List the watched mangas",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		svc, err := newWatchManager(cmd)
		if err != nil {
			return err
		}
		watches, err := svc.List(cmd.Context())
		if err != nil {
			return err
		}
		if watchLsArgs.JSON {
			if watches == nil {
				watches = []watch.Watch{}
			}
			return printJSON(cmd.OutOrStdout(), watches)
		}
		if len(watches) == 0 {
			cmd.Println(`no watches, "mangal watch add" adds one`)
			return nil
		}
		printWatches(cmd.OutOrStdout(), watches)
		return nil
	},
}

func printWatches(out io.Writer, watches []watch.Watch) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tPROVIDER\tTITLE\tLAST\tCHECKED\tNEXT\tERRORS")
	for _, w := range watches {
		next := formatTime(w.NextCheck)
		if !w.Enabled {
			next = "paused"
		}
		errs := "-"
		if w.Failures > 0 {
			errs = fmt.Sprintf("%d: %s", w.Failures, firstLine(w.LastError, 60))
		} else if w.LastError != "" {
			errs = firstLine(w.LastError, 60)
		}
		title := w.Title
		if w.Gaps {
			title += " (gaps)"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n", w.ID, w.Provider, title, formatLast(w.LastDownloaded), formatTime(w.LastChecked), next, errs)
	}
	_ = tw.Flush()
}

// firstLine is the first line of s, cut to max runes.
func firstLine(s string, max int) string {
	s, _, _ = strings.Cut(s, "\n")
	if r := []rune(s); len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

func init() {
	watchCmd.AddCommand(watchRmCmd)
}

var watchRmCmd = &cobra.Command{
	Use:     "rm <id>...",
	Aliases: []string{"remove"},
	Short:   "Stop watching mangas",
	Long:    `Stop watching the mangas, the history of their checks stays (mangal watch log).`,
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ids, err := parseIDs(args)
		if err != nil {
			return err
		}
		svc, err := newWatchManager(cmd)
		if err != nil {
			return err
		}
		for _, id := range ids {
			w, err := svc.Get(cmd.Context(), id)
			if err != nil {
				return err
			}
			if err := svc.Remove(cmd.Context(), id); err != nil {
				return err
			}
			successf(cmd, "stopped watching %q from %s", w.Title, w.Provider)
		}
		return nil
	},
}

func init() {
	watchCmd.AddCommand(pauseCmd(false), pauseCmd(true))
}

func pauseCmd(resume bool) *cobra.Command {
	use, short, done := "pause", "Pause watches (not checked until resumed)", "paused"
	if resume {
		use, short, done = "resume", "Resume paused watches", "resumed"
	}
	return &cobra.Command{
		Use:   use + " <id>...",
		Short: short,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, err := parseIDs(args)
			if err != nil {
				return err
			}
			svc, err := newWatchManager(cmd)
			if err != nil {
				return err
			}
			for _, id := range ids {
				w, err := svc.SetEnabled(cmd.Context(), id, resume)
				if err != nil {
					return err
				}
				successf(cmd, "%s %q from %s", done, w.Title, w.Provider)
			}
			return nil
		},
	}
}

var watchSetArgs = struct {
	Interval  time.Duration
	Gaps      bool
	AnilistID int
	From      float32
}{}

func init() {
	watchCmd.AddCommand(watchSetCmd)
	f := watchSetCmd.Flags()
	f.DurationVar(&watchSetArgs.Interval, "interval", 0, "Check interval of this manga, 0 for config watch.interval")
	f.BoolVar(&watchSetArgs.Gaps, "gaps", false, "Gap filling (--gaps=false turns it off)")
	f.IntVarP(&watchSetArgs.AnilistID, "anilist-id", "a", 0, "AniList manga to bind, 0 unbinds")
	f.Float32Var(&watchSetArgs.From, "from", 0, "Treat the chapters up to this number as downloaded")
}

var watchSetCmd = &cobra.Command{
	Use:   "set <id>",
	Short: "Change a watch's settings",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := parseID(args[0])
		if err != nil {
			return err
		}
		var opts watch.UpdateOptions
		f := cmd.Flags()
		if f.Changed("interval") {
			opts.Interval = &watchSetArgs.Interval
		}
		if f.Changed("gaps") {
			opts.Gaps = &watchSetArgs.Gaps
		}
		if f.Changed("anilist-id") {
			opts.AnilistID = &watchSetArgs.AnilistID
		}
		if f.Changed("from") {
			opts.From = &watchSetArgs.From
		}
		if opts == (watch.UpdateOptions{}) {
			return errors.New("nothing to change, see --help")
		}
		svc, err := newWatchManager(cmd)
		if err != nil {
			return err
		}
		w, err := svc.Update(cmd.Context(), id, opts)
		if err != nil {
			return err
		}
		successf(cmd, "updated %q from %s, next check %s", w.Title, w.Provider, formatTime(w.NextCheck))
		return nil
	},
}

var watchCheckArgs = struct {
	DryRun bool
	Gaps   bool
	JSON   bool
}{}

func init() {
	watchCmd.AddCommand(watchCheckCmd)
	f := watchCheckCmd.Flags()
	f.BoolVarP(&watchCheckArgs.DryRun, "dry-run", "n", false, "Only list what would be downloaded")
	f.BoolVar(&watchCheckArgs.Gaps, "gaps", false, "Also download the missing chapters below the last downloaded one, for every watch checked")
	f.BoolVar(&watchCheckArgs.JSON, "json", false, "Print the report as JSON at the end (instead of the lines)")
}

var watchCheckCmd = &cobra.Command{
	Use:   "check [id...]",
	Short: "Check the watches now",
	Long: `Check the watches with the ids, or every one that isn't paused, and download what's new.
Only one check runs at a time (also across processes, e.g. "mangal serve"). Errors are
sent to Discord as configured (watch.notify), and make the exit code 1.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ids, err := parseIDs(args)
		if err != nil {
			return err
		}
		svc, err := newWatchManager(cmd)
		if err != nil {
			return err
		}
		kind := watch.KindManual
		if watchCheckArgs.Gaps {
			kind = watch.KindGaps
		}
		opts := watch.CheckOptions{Kind: kind, DryRun: watchCheckArgs.DryRun, Gaps: watchCheckArgs.Gaps}
		if !watchCheckArgs.JSON {
			opts.OnEvent = func(e watch.Event) { printCheckEvent(cmd, e, watchCheckArgs.DryRun) }
		}
		report, err := svc.Check(cmd.Context(), ids, opts)
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
			// the server doesn't tell the progress, only what it recorded
			for _, r := range report.Results {
				printResult(cmd, r, watchCheckArgs.DryRun)
			}
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
	switch e.Kind {
	case watch.EventChapter:
		if c := e.Chapter; c.Kind == client.EventWait {
			cmd.PrintErrf("%s: chapter %s rate limited (retry #%d), retrying in %s\n", e.Watch.Title, c.Result.Chapter, c.Attempt, c.Wait)
		}
	case watch.EventDone:
		r := e.Result
		printResult(cmd, *r, dryRun)
	}
}

var watchLogArgs = struct {
	Since string
	Watch int64
	Limit int
	JSON  bool
}{}

func init() {
	watchCmd.AddCommand(watchLogCmd)
	f := watchLogCmd.Flags()
	f.StringVar(&watchLogArgs.Since, "since", "7d", "How far back (e.g. 7d, 12h), 0 for everything")
	f.Int64Var(&watchLogArgs.Watch, "watch", 0, "Only the checks of this watch id")
	f.IntVar(&watchLogArgs.Limit, "limit", 0, "Only the latest N check runs")
	f.BoolVar(&watchLogArgs.JSON, "json", false, "Print JSON")
}

var watchLogCmd = &cobra.Command{
	Use:   "log",
	Short: "Show the history of the checks",
	Long:  `Show the check runs, latest first, with what each watch got: the chapters downloaded, nothing new or the error.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		since, err := parseSince(watchLogArgs.Since)
		if err != nil {
			return err
		}
		svc, err := newWatchManager(cmd)
		if err != nil {
			return err
		}
		filter := watch.RunFilter{Watch: watchLogArgs.Watch, Limit: watchLogArgs.Limit}
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
		for i := len(runs) - 1; i >= 0; i-- {
			printRun(cmd, runs[i])
		}
		return nil
	},
}

func printRun(cmd *cobra.Command, run watch.CheckRun) {
	took := "unfinished"
	if !run.Finished.IsZero() {
		took = run.Finished.Sub(run.Started).Round(time.Second).String()
	}
	cmd.Printf("# %s check %d, %s (%s): %s, %s, %s\n", run.Kind, run.ID, formatTime(run.Started), took,
		stringutil.Quantify(run.Watches, "watch", "watches"), stringutil.Quantify(run.NewChapters, "new chapter", "new chapters"),
		stringutil.Quantify(run.Errors, "error", "errors"))
	for _, r := range run.Results {
		from := "all"
		if r.From != nil {
			from = stringutil.FormatFloa32(*r.From) + "-"
		}
		line := fmt.Sprintf("  %s (%s) [%s]: ", r.Title, r.Provider, from)
		switch r.Status {
		case storage.StatusNew:
			line += fmt.Sprintf("%d new", len(r.Chapters))
		case storage.StatusNone:
			line += "no new chapters"
		default:
			line += r.Status
		}
		if r.Error != "" {
			line += ": " + r.Error
		}
		cmd.Println(line)
		for _, ch := range r.Chapters {
			cmd.Printf("    %s  %s\n", stringutil.FormatFloa32(ch.Number), ch.Path)
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
	watchStaleCmd.Flags().BoolVar(&watchStaleArgs.JSON, "json", false, "Print JSON")
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
			last := "no new chapter since added " + formatTime(w.Added)
			if !w.LastNewAt.IsZero() {
				days := int(time.Since(w.LastNewAt).Hours() / 24)
				last = fmt.Sprintf("last new chapter %s on %s (%s ago)", formatLast(w.LastNew), formatTime(w.LastNewAt), stringutil.Quantify(days, "day", "days"))
			}
			cmd.Printf("%d  %s (%s): %s\n", w.ID, w.Title, w.Provider, last)
		}
		return nil
	},
}
