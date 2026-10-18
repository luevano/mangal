package cmd

import (
	"context"
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
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/notify"
	"github.com/luevano/mangal/v5/util/cron"
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

var watchAddArgs = struct {
	Offline   bool
	ID        string
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
	f.StringVar(&watchAddArgs.ID, "id", "", `The manga's id (as "mangal inline json" shows it), instead of a title query`)
	f.BoolVar(&watchAddArgs.Offline, "offline", false, "Add it as given (--id, --title) without asking the provider, which needn't be installed; paused unless --enabled")
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
	Use:   "add <provider> [title]",
	Short: "Watch a manga",
	Long: `Watch a manga of the provider: the one the manga selector picks from a search of the
title (an exact match by default), or the manga with the --id (as "mangal inline json"
shows it). Its first check downloads every chapter the library doesn't have, or the ones
after --from.`,
	Example: `  mangal watch add mango-mangapill "One-Punch Man" --from 200
  mangal watch add mango-mangaplus --id 100171 --anilist-id 132029`,
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completionFirstProvider,
	RunE: func(cmd *cobra.Command, args []string) error {
		schedule, _, err := scheduleFlag(cmd.Flags(), watchAddArgs.Schedule, watchAddArgs.Interval)
		if err != nil {
			return err
		}
		mangaID, query, err := mangaQuery(cmd, "id", watchAddArgs.ID, strings.Join(args[1:], " "))
		if err != nil {
			return err
		}
		if mangaID == "" && query == "" {
			return errors.New("a title or --id")
		}
		svc, err := newWatchManager(cmd)
		if err != nil {
			return err
		}
		opts := watch.AddOptions{
			Provider:  args[0],
			MangaID:   mangaID,
			Query:     query,
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
			state = ", " + w.RelinkLabel() + " (mangal watch move)"
		case !w.Enabled:
			state = ", paused"
		case w.ProviderMissing:
			state = ", provider missing"
		}
		successf(cmd, "watching %q from %s (id %d), last downloaded %s%s", w.Title, w.Provider, w.ID, watch.FormatNumber(w.LastDownloaded), state)
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
		next := watch.NextLabel(w, now, config.Watch.Backoff.Get())
		if w.NeedsRelink {
			next += ": " + firstLine(w.RelinkReason, 50)
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
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n", w.ID, w.Provider, title, watch.FormatNumber(w.LastDownloaded), watch.FormatTime(w.LastChecked), next, errs)
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
		if w.Coverage == "" || w.NeedsRelink {
			continue
		}
		if first {
			fmt.Fprintln(out, "\nanother provider? (mangal watch move <id> <provider>, or mangal watch hold <id> provider):")
			first = false
		}
		fmt.Fprintf(out, "  %d  %s: %s\n", w.ID, firstLine(w.Title, 50), w.Coverage)
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
	MangaID   string
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
	f.StringVar(&watchSetArgs.Manga, "manga", "", `Re-link it to the manga of its provider with this exact title, see mangal watch move`)
	f.StringVar(&watchSetArgs.MangaID, "manga-id", "", `Re-link it to the manga of its provider with this id, see mangal watch move`)
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
		relinkID, relink, err := mangaQuery(cmd, "manga-id", watchSetArgs.MangaID, watchSetArgs.Manga)
		if err != nil {
			return err
		}
		if opts == (watch.UpdateOptions{}) && relink == "" && relinkID == "" && changeSkip == nil {
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
		if relink != "" || relinkID != "" {
			if w, err = svc.Relink(cmd.Context(), id, watch.RelinkOptions{MangaID: relinkID, Query: relink}); err != nil {
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
	ID        string
	Selector  string
	AnilistID int
}{}

func init() {
	watchCmd.AddCommand(watchMoveCmd)
	f := watchMoveCmd.Flags()
	f.StringVar(&watchMoveArgs.ID, "id", "", "The manga's id, instead of a title query")
	f.StringVarP(&watchMoveArgs.Selector, "manga-selector", "m", "exact", "Manga selector of the search (first|last|exact|closest|<index>)")
	f.IntVarP(&watchMoveArgs.AnilistID, "anilist-id", "a", 0, "AniList manga to bind as the default too (the bindings move with the watch anyway)")
}

var watchMoveCmd = &cobra.Command{
	Use:     "move <id> [provider] [title]",
	Aliases: []string{"relink"},
	Short:   "Point a watch to another manga, keeping the watch",
	Long: `Point the watch to another manga: of another provider, or of the same one when the
site changed its manga ids. The watch keeps its id, the history of its checks, its schedule
and settings, its metadata bindings and its library manga, and no longer needs a re-link.

The manga is picked like "mangal watch add" does: the one with the --id, or the one the
manga selector picks from a search of the title. Without a provider it's the watch's;
without a title or --id, the watch's title (an exact match).`,
	Example: `  mangal watch move 12
  mangal watch move 12 mango-weebcentral
  mangal watch move 12 mango-weebcentral "Berserk"
  mangal watch move 12 --id 01J76XYCERW3TT8A0PSB0QUPZT`,
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completionMoveArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := parseID(args[0])
		if err != nil {
			return err
		}
		provider, query := moveArgs(args[1:], isProvider())
		mangaID, query, err := mangaQuery(cmd, "id", watchMoveArgs.ID, query)
		if err != nil {
			return err
		}
		svc, err := newWatchManager(cmd)
		if err != nil {
			return err
		}
		before, err := svc.Get(cmd.Context(), id)
		if err != nil {
			return err
		}
		if query == "" && mangaID == "" {
			query = before.Title
		}
		w, err := svc.Relink(cmd.Context(), id, watch.RelinkOptions{Provider: provider, MangaID: mangaID, Query: query,
			Selector: watchMoveArgs.Selector, AnilistID: watchMoveArgs.AnilistID})
		if err != nil {
			return err
		}
		successf(cmd, "watch %d: %q from %s (manga id %s), was %s %s, last downloaded %s", w.ID, w.Title, w.Provider, w.MangaID,
			before.Provider, before.MangaID, watch.FormatNumber(w.LastDownloaded))
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
	infos, err := core.NewProviders(nil).List(context.Background())
	if err != nil {
		return func(string) bool { return true }
	}
	return func(id string) bool {
		return slices.ContainsFunc(infos, func(info libmangal.ProviderInfo) bool {
			_, old := info.Alias(id)
			return info.ID == id || old
		})
	}
}

func completionMoveArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 1 {
		return completionProviderIDs(cmd, args, toComplete)
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}
