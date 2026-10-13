package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/notify"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cron"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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
timer), "mangal serve" on a cron schedule (watch.schedule, or the watch's own).`,
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
		if err != nil || !(n >= 0) || math.IsInf(n, 0) {
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
	Offline   bool
	Title     string
	Enabled   bool
	Selector  string
	AnilistID int
	From      float32
	Gaps      bool
	Schedule  string
	Interval  time.Duration
}{}

func init() {
	watchCmd.AddCommand(watchAddCmd)
	f := watchAddCmd.Flags()
	f.BoolVar(&watchAddArgs.Offline, "offline", false, "Add it as given (mid:<id>, --title) without asking the provider, which needn't be installed; paused unless --enabled")
	f.StringVar(&watchAddArgs.Title, "title", "", "Title of an --offline watch (default: the id)")
	f.BoolVar(&watchAddArgs.Enabled, "enabled", false, "An --offline watch starts enabled (checked once its provider is installed)")
	f.StringVarP(&watchAddArgs.Selector, "manga-selector", "m", "exact", "Manga selector of the search (first|last|exact|closest|<index>)")
	f.IntVarP(&watchAddArgs.AnilistID, "anilist-id", "a", 0, "AniList manga to bind as the default (its metadata is used for the downloads, see mangal bind)")
	f.Float32Var(&watchAddArgs.From, "from", 0, "Treat the chapters up to this number as downloaded (read elsewhere)")
	f.BoolVar(&watchAddArgs.Gaps, "gaps", false, "Also download the missing chapters below the last downloaded one (default: config watch.gaps)")
	f.StringVar(&watchAddArgs.Schedule, "schedule", "", `When this manga is checked, a cron schedule such as "0 */6 * * *" or @daily (default: config watch.schedule)`)
	intervalAlias(f, &watchAddArgs.Interval)
}

// intervalAlias is the old --interval, hidden: a duration that becomes the closest cron
// schedule (cron.FromInterval).
func intervalAlias(f *pflag.FlagSet, d *time.Duration) {
	f.DurationVar(d, "interval", 0, "Replaced by --schedule")
	_ = f.MarkHidden("interval")
}

// scheduleFlag is --schedule, or the --interval alias as a schedule; changed is false
// when neither was given.
func scheduleFlag(f *pflag.FlagSet, schedule string, interval time.Duration) (string, bool, error) {
	switch {
	case f.Changed("schedule") && f.Changed("interval"):
		return "", false, errors.New("--interval is the old --schedule, use one")
	case f.Changed("schedule"):
		if _, err := watch.ParseSchedule(schedule); err != nil {
			return "", false, err
		}
		return strings.TrimSpace(schedule), true, nil
	case f.Changed("interval"):
		return cron.FromInterval(interval), true, nil
	}
	return "", false, nil
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
	ValidArgsFunction: completionFirstProvider,
	RunE: func(cmd *cobra.Command, args []string) error {
		schedule, _, err := scheduleFlag(cmd.Flags(), watchAddArgs.Schedule, watchAddArgs.Interval)
		if err != nil {
			return err
		}
		svc, err := newWatchManager(cmd)
		if err != nil {
			return err
		}
		opts := watch.AddOptions{
			Provider:  args[0],
			Query:     strings.Join(args[1:], " "),
			Selector:  watchAddArgs.Selector,
			AnilistID: watchAddArgs.AnilistID,
			Schedule:  schedule,
			Offline:   watchAddArgs.Offline,
			Title:     watchAddArgs.Title,
		}
		if watchAddArgs.Offline {
			opts.Enabled = &watchAddArgs.Enabled
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
		state := ""
		switch {
		case w.NeedsRelink:
			state = ", needs re-link (mangal watch move)"
		case !w.Enabled:
			state = ", paused"
		case w.ProviderMissing:
			state = ", provider missing"
		}
		successf(cmd, "watching %q from %s (id %d), last downloaded %s%s", w.Title, w.Provider, w.ID, formatLast(w.LastDownloaded), state)
		if w.LibraryProblem() != "" {
			cmd.Printf("library: %s\n", w.LibraryProblem())
		}
		if len(w.Bindings) != 0 {
			cmd.Printf("metadata: %s\n", formatBindings(w.Bindings))
		}
		return nil
	},
}

var watchLsArgs = struct {
	JSON bool
}{}

func init() {
	watchCmd.AddCommand(watchLsCmd)
	watchLsCmd.Flags().BoolVarP(&watchLsArgs.JSON, "json", "j", false, "Print JSON")
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
	now := time.Now()
	for _, w := range watches {
		next := watch.FormatNext(w.NextScheduled, now)
		switch {
		case w.NeedsRelink:
			next = "needs re-link: " + firstLine(w.RelinkReason, 50)
		case !w.Enabled && w.ProviderMissing:
			next = "paused, provider missing"
		case !w.Enabled:
			next = "paused"
		case w.ProviderMissing:
			next = "provider missing"
		case w.LibraryHold() != "":
			next = w.LibraryHold()
		}
		errs := "-"
		if w.Failures > 0 {
			errs = fmt.Sprintf("%d: %s", w.Failures, firstLine(w.LastError, 60))
		} else if w.LastError != "" {
			errs = firstLine(w.LastError, 60)
		}
		title := firstLine(w.Title, 50)
		if w.Gaps {
			title += " (gaps)"
		}
		if w.LibraryProblem() != "" {
			title += " (not in library)"
		}
		// ongoing is what's expected of a watch
		if status := library.StatusLabel(w.Status); status != "" && status != "ongoing" {
			title += " (" + status + ")"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n", w.ID, w.Provider, title, formatLast(w.LastDownloaded), formatTime(w.LastChecked), next, errs)
	}
	_ = tw.Flush()
	first := true
	for _, w := range watches {
		if w.LibraryProblem() == "" {
			continue
		}
		if first {
			fmt.Fprintln(out, "\nnot in the library:")
			first = false
		}
		fmt.Fprintf(out, "  %d  %s: %s\n", w.ID, firstLine(w.Title, 50), w.LibraryProblem())
	}
	first = true
	for _, w := range watches {
		if len(w.Skip) == 0 {
			continue
		}
		if first {
			fmt.Fprintln(out, "\nnever downloaded (skip list):")
			first = false
		}
		fmt.Fprintf(out, "  %d  %s: %s\n", w.ID, firstLine(w.Title, 50), watch.FormatNumbers(w.Skip))
	}
}

// firstLine is the first line of s, cut to max runes.
func firstLine(s string, max int) string {
	s, _, _ = strings.Cut(s, "\n")
	if r := []rune(s); len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

var watchRmArgs = struct {
	Apply bool
}{}

func init() {
	watchCmd.AddCommand(watchRmCmd)
	watchRmCmd.Flags().BoolVar(&watchRmArgs.Apply, "apply", false, "Stop watching them, instead of only listing them")
}

var watchRmCmd = &cobra.Command{
	Use:     "rm <id>...",
	Aliases: []string{"remove"},
	Short:   "Stop watching mangas",
	Long: `Stop watching the mangas, the history of their checks stays (mangal watch log). The data
database is snapshotted first: "mangal library undo" brings them back.

Only lists them unless --apply is set.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ids, err := parseIDs(args)
		if err != nil {
			return err
		}
		svc, err := newWatchManager(cmd)
		if err != nil {
			return err
		}
		var watches []watch.Watch
		for _, id := range ids {
			w, err := svc.Get(cmd.Context(), id)
			if err != nil {
				return err
			}
			watches = append(watches, w)
		}
		if !watchRmArgs.Apply {
			for _, w := range watches {
				cmd.Printf("would stop watching %d %q from %s\n", w.ID, w.Title, w.Provider)
			}
			cmd.Println("run with --apply to stop watching them")
			return nil
		}
		if local, ok := svc.(*watch.Service); ok {
			snapshot, err := local.RemoveWatches(cmd.Context(), ids)
			undoHint(cmd, snapshot, err)
			if err != nil {
				return err
			}
		} else {
			// the server journals each one
			for _, id := range ids {
				if err := svc.Remove(cmd.Context(), id); err != nil {
					return err
				}
			}
		}
		for _, w := range watches {
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
	var provider string
	c := &cobra.Command{
		Use:   use + " <id>... | --provider <id>",
		Short: short,
		Long:  short + `; --provider for every watch of a provider (e.g. while the site is broken).`,
		Args: func(cmd *cobra.Command, args []string) error {
			if provider == "" {
				return cobra.MinimumNArgs(1)(cmd, args)
			}
			return cobra.NoArgs(cmd, args)
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
			if provider != "" {
				all, err := svc.List(cmd.Context())
				if err != nil {
					return err
				}
				for _, w := range all {
					if w.Provider == provider && w.Enabled != resume {
						ids = append(ids, w.ID)
					}
				}
				if len(ids) == 0 {
					successf(cmd, "no watches of %s to %s", provider, use)
					return nil
				}
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
	c.Flags().StringVar(&provider, "provider", "", "Every watch of this provider")
	_ = c.RegisterFlagCompletionFunc("provider", completionProviderIDs)
	return c
}

var watchSetArgs = struct {
	Manga     string
	Schedule  string
	Interval  time.Duration
	Gaps      bool
	AnilistID int
	From      float32
	Skip      string
	Unskip    string
}{}

func init() {
	watchCmd.AddCommand(watchSetCmd)
	f := watchSetCmd.Flags()
	f.StringVar(&watchSetArgs.Schedule, "schedule", "", `When this manga is checked, a cron schedule such as "0 */6 * * *" or @daily, "" for config watch.schedule`)
	intervalAlias(f, &watchSetArgs.Interval)
	f.BoolVar(&watchSetArgs.Gaps, "gaps", false, "Gap filling (--gaps=false turns it off)")
	f.IntVarP(&watchSetArgs.AnilistID, "anilist-id", "a", 0, "AniList manga to bind as the default, 0 unbinds it (see mangal bind)")
	f.Float32Var(&watchSetArgs.From, "from", 0, "Treat the chapters up to this number as downloaded")
	f.StringVar(&watchSetArgs.Manga, "manga", "", `Re-link it to this manga of its provider ("mid:<id>" or an exact title), see mangal watch move`)
	f.StringVar(&watchSetArgs.Skip, "skip", "", `Never download these chapter numbers (e.g. a fake "189.5,190"), added to its skip list`)
	f.StringVar(&watchSetArgs.Unskip, "unskip", "", `Remove these chapter numbers from its skip list, "all" clears it`)
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
		schedule, changed, err := scheduleFlag(f, watchSetArgs.Schedule, watchSetArgs.Interval)
		if err != nil {
			return err
		}
		if changed {
			opts.Schedule = &schedule
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
		changeSkip, err := skipFlags(f, watchSetArgs.Skip, watchSetArgs.Unskip)
		if err != nil {
			return err
		}
		relink := strings.TrimSpace(watchSetArgs.Manga)
		if opts == (watch.UpdateOptions{}) && relink == "" && changeSkip == nil {
			return errors.New("nothing to change, see --help")
		}
		svc, err := newWatchManager(cmd)
		if err != nil {
			return err
		}
		if changeSkip != nil {
			w, err := svc.Get(cmd.Context(), id)
			if err != nil {
				return err
			}
			list := changeSkip(w.Skip)
			if list == nil {
				list = []float32{}
			}
			opts.Skip = &list
		}
		var w watch.Watch
		if relink != "" {
			if w, err = svc.Relink(cmd.Context(), id, watch.RelinkOptions{Query: relink}); err != nil {
				return err
			}
			successf(cmd, "re-linked watch %d to %q from %s (manga id %s)", w.ID, w.Title, w.Provider, w.MangaID)
			if opts == (watch.UpdateOptions{}) {
				return nil
			}
		}
		if w, err = svc.Update(cmd.Context(), id, opts); err != nil {
			return err
		}
		successf(cmd, "updated %q from %s, next check %s", w.Title, w.Provider, watch.FormatNext(w.NextScheduled, time.Now()))
		if opts.Skip != nil {
			if len(w.Skip) == 0 {
				cmd.Println("skip list: empty")
			} else {
				cmd.Println("skip list: " + watch.FormatNumbers(w.Skip))
			}
		}
		return nil
	},
}

// skipFlags change the skip list by --skip (adds) and --unskip (removes, "all" clears),
// nil when neither is given.
func skipFlags(f *pflag.FlagSet, skip, unskip string) (func([]float32) []float32, error) {
	if !f.Changed("skip") && !f.Changed("unskip") {
		return nil, nil
	}
	var add, remove []float32
	clearAll := false
	if f.Changed("skip") {
		var err error
		if add, err = watch.ParseNumbers(skip); err != nil {
			return nil, fmt.Errorf("--skip: %w", err)
		}
		if len(add) == 0 {
			return nil, errors.New(`--skip: no chapter numbers (--unskip all clears the list)`)
		}
	}
	if f.Changed("unskip") {
		if clearAll = strings.TrimSpace(unskip) == "all"; !clearAll {
			var err error
			if remove, err = watch.ParseNumbers(unskip); err != nil {
				return nil, fmt.Errorf("--unskip: %w", err)
			}
			if len(remove) == 0 {
				return nil, errors.New(`--unskip: no chapter numbers ("all" clears the list)`)
			}
		}
	}
	return func(list []float32) []float32 {
		if clearAll {
			list = nil
		}
		return watch.RemoveNumbers(watch.AddNumbers(list, add), remove)
	}, nil
}

var watchMoveArgs = struct {
	Selector  string
	AnilistID int
}{}

func init() {
	watchCmd.AddCommand(watchMoveCmd)
	f := watchMoveCmd.Flags()
	f.StringVarP(&watchMoveArgs.Selector, "manga-selector", "m", "exact", "Manga selector of the search (first|last|exact|closest|<index>)")
	f.IntVarP(&watchMoveArgs.AnilistID, "anilist-id", "a", 0, "AniList manga to bind as the default too (the bindings move with the watch anyway)")
}

var watchMoveCmd = &cobra.Command{
	Use:     "move <id> [provider] [query|mid:id]",
	Aliases: []string{"relink"},
	Short:   "Point a watch to another manga, keeping the watch",
	Long: `Point the watch to another manga: of another provider, or of the same one when the
site changed its manga ids. The watch keeps its id, the history of its checks, its schedule
and settings, its metadata bindings and its library manga, and no longer needs a re-link.

The manga is picked like "mangal watch add" does: the one with the id ("mid:<id>"), or
the one the manga selector picks from a search of the query. Without a provider it's the
watch's; without a query, the watch's title (an exact match).`,
	Example: `  mangal watch move 12
  mangal watch move 12 mango-weebcentral
  mangal watch move 12 mango-weebcentral "Berserk"
  mangal watch move 12 mid:01J76XYCERW3TT8A0PSB0QUPZT`,
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completionMoveArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := parseID(args[0])
		if err != nil {
			return err
		}
		provider, query := moveArgs(args[1:], isProvider())
		svc, err := newWatchManager(cmd)
		if err != nil {
			return err
		}
		before, err := svc.Get(cmd.Context(), id)
		if err != nil {
			return err
		}
		if query == "" {
			query = before.Title
		}
		w, err := svc.Relink(cmd.Context(), id, watch.RelinkOptions{Provider: provider, Query: query,
			Selector: watchMoveArgs.Selector, AnilistID: watchMoveArgs.AnilistID})
		if err != nil {
			return err
		}
		successf(cmd, "watch %d: %q from %s (manga id %s), was %s %s, last downloaded %s", w.ID, w.Title, w.Provider, w.MangaID,
			before.Provider, before.MangaID, formatLast(w.LastDownloaded))
		if len(w.Bindings) != 0 {
			cmd.Printf("metadata: %s\n", formatBindings(w.Bindings))
		}
		return nil
	},
}

// moveArgs are the provider and query of "watch move": the first argument is the provider
// when it's one (isProvider), the rest the query.
func moveArgs(args []string, isProvider func(string) bool) (provider, query string) {
	if len(args) > 0 && (len(args) > 1 || isProvider(args[0])) {
		provider, args = args[0], args[1:]
	}
	return provider, strings.Join(args, " ")
}

// isProvider tells the installed providers' ids (old ids too); every id is one when the
// providers can't be loaded (the error comes when the watch service needs them).
func isProvider() func(string) bool {
	loaders, err := manager.Loaders()
	if err != nil {
		return func(string) bool { return true }
	}
	return func(id string) bool {
		_, _, ok := libmangal.FindLoader(loaders, id)
		return ok
	}
}

func completionMoveArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 1 {
		return completionProviderIDs(cmd, args, toComplete)
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

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
		for i := len(runs) - 1; i >= 0; i-- {
			printRun(cmd, runs[i])
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
		successf(cmd, "deleted run %d (%s from %s, %s check %d, %s) from the history", r.ID, r.Title, r.Provider, r.Status, r.CheckRun, formatTime(r.Started))
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
	cmd.Printf("# %s check %d, %s (%s): %s, %s, %s\n", run.Kind, run.ID, formatTime(run.Started), took,
		stringutil.Quantify(run.Watches, "watch", "watches"), stringutil.Quantify(run.NewChapters, "new chapter", "new chapters"),
		stringutil.Quantify(run.Errors, "error", "errors"))
	for _, r := range run.Results {
		from := "all"
		if r.From != nil {
			from = stringutil.FormatFloa32(*r.From) + "-"
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
