package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/path"
	"github.com/spf13/cobra"
)

type logsFlags struct {
	List          bool
	All           bool
	Files         []string
	Since         string
	Until         string
	Level         string
	Grep          string
	CaseSensitive bool
	Last          int
	Follow        bool
	JSON          bool
}

var logsArgs logsFlags

func init() {
	rootCmd.AddCommand(logsCmd)
	f := logsCmd.Flags()
	f.BoolVarP(&logsArgs.List, "list", "l", false, "List the log files")
	f.BoolVarP(&logsArgs.All, "all", "a", false, "Read every log file, not only the current period's")
	f.StringSliceVar(&logsArgs.Files, "file", nil, "Read these log files (names as in --list)")
	f.StringVar(&logsArgs.Since, "since", "", "Entries since a time back from now (2h, 7d) or a date/time (2026-10-12, 2026-10-12T15:04)")
	f.StringVar(&logsArgs.Until, "until", "", "Entries until a time back from now or a date/time (a date includes its day)")
	f.StringVarP(&logsArgs.Level, "level", "L", "debug", "Lowest level shown: debug, info, warn, error")
	f.StringVarP(&logsArgs.Grep, "grep", "g", "", "Entries matching this regexp (case-insensitive unless it has a capital letter)")
	f.BoolVar(&logsArgs.CaseSensitive, "case-sensitive", false, "--grep is case-sensitive")
	f.IntVarP(&logsArgs.Last, "lines", "n", 0, "Only the last N entries (10 with --follow)")
	f.BoolVarP(&logsArgs.Follow, "follow", "f", false, "Keep printing new entries (ctrl+c stops)")
	f.BoolVarP(&logsArgs.JSON, "json", "j", false, "Print JSON (one object per entry)")
	logsCmd.MarkFlagsMutuallyExclusive("all", "file")
	_ = logsCmd.RegisterFlagCompletionFunc("level", cobra.FixedCompletions([]string{"debug", "info", "warn", "error"}, cobra.ShellCompDirectiveNoFileComp))
	_ = logsCmd.RegisterFlagCompletionFunc("file", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		files, _ := core.NewLogs(path.Logs).Files(context.Background())
		names := make([]string, len(files))
		for i, f := range files {
			names[i] = f.Name
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	})
}

var logsCmd = &cobra.Command{
	Annotations: map[string]string{startupAnnotation: startupNoStorage},
	Use:         "logs",
	Short:       "Show and search the logs",
	Long: `Show the log entries of the current period's files (log.rotate), or of every file
(--all, or when --since/--until is given), or of the named ones (--file). Compressed files
are read too. --follow keeps printing the new entries, of every mangal process.`,
	Example: `  mangal logs -L warn --since 2d
  mangal logs -a -g 'mangadex.*timeout'
  mangal logs -f -n 20`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runLogs(cmd, path.Logs(), time.Now())
	},
}

func runLogs(cmd *cobra.Command, dir string, now time.Time) error {
	logs := core.NewLogs(func() string { return dir })
	ctx := ctxOf(cmd)
	files, err := logs.Files(ctx)
	if err != nil {
		return err
	}
	if logsArgs.List {
		return listLogFiles(cmd, files)
	}

	q, err := logsQuery(now)
	if err != nil {
		return err
	}
	if files, err = pickLogFiles(files, now); err != nil {
		return err
	}
	for _, f := range files {
		q.Files = append(q.Files, f.Name)
	}

	show := func(e log.Entry) error {
		if logsArgs.JSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(logEntryJSON{e.Time, e.Level.String(), e.Message, e.File})
		}
		cmd.Println(e.String())
		return nil
	}
	q.Limit = logsArgs.Last
	if logsArgs.Follow && !cmd.Flags().Changed("lines") {
		q.Limit = 10
	}
	n := 0
	// no files (none of the current period) is nothing to read, no names would be every file
	if len(files) != 0 {
		err = logs.Entries(ctx, q, func(e log.Entry) error {
			n++
			return show(e)
		})
	}
	if err != nil && !errors.Is(err, ctx.Err()) {
		return fmt.Errorf("read logs: %w", err)
	}
	if logsArgs.Follow {
		return logs.Follow(ctx, q, show)
	}
	if n == 0 && !logsArgs.JSON {
		hint := ""
		if !logsArgs.All && len(logsArgs.Files) == 0 && logsArgs.Since == "" && logsArgs.Until == "" {
			hint = " in the current period (--all reads every file)"
		}
		cmd.PrintErrf("no log entries%s\n", hint)
	}
	return nil
}

type logEntryJSON struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
	File    string    `json:"file"`
}

func listLogFiles(cmd *cobra.Command, files []log.File) error {
	if logsArgs.JSON {
		type file struct {
			Name       string    `json:"name"`
			Path       string    `json:"path"`
			Size       int64     `json:"size"`
			Modified   time.Time `json:"modified"`
			Closed     bool      `json:"closed"`
			Compressed bool      `json:"compressed"`
		}
		out := make([]file, len(files))
		for i, f := range files {
			out[i] = file{f.Name, f.Path, f.Size, f.ModTime, f.Closed, f.Compressed}
		}
		return printJSON(cmd.OutOrStdout(), out)
	}
	if len(files) == 0 {
		cmd.Println("no log files")
		return nil
	}
	width := 0
	for _, f := range files {
		width = max(width, len(f.Name))
	}
	for _, f := range files {
		state := "open"
		if f.Closed {
			state = "closed"
		}
		cmd.Printf("%-*s  %8s  %s  %s\n", width, f.Name, humanize.Bytes(uint64(f.Size)), watch.FormatTime(f.ModTime), state)
	}
	return nil
}

// logsQuery is the query of the flags (the files picked apart).
func logsQuery(now time.Time) (core.LogQuery, error) {
	q := core.LogQuery{Level: logsArgs.Level, Grep: logsArgs.Grep, CaseSensitive: logsArgs.CaseSensitive}
	if _, err := log.ParseLevel(logsArgs.Level); err != nil {
		return q, err
	}
	var err error
	if q.Since, err = parseLogTime(logsArgs.Since, now, false); err != nil {
		return q, fmt.Errorf("--since: %w", err)
	}
	if q.Until, err = parseLogTime(logsArgs.Until, now, true); err != nil {
		return q, fmt.Errorf("--until: %w", err)
	}
	return q, nil
}

// pickLogFiles are the files the flags read.
func pickLogFiles(files []log.File, now time.Time) ([]log.File, error) {
	if len(logsArgs.Files) > 0 {
		var picked []log.File
		for _, name := range logsArgs.Files {
			i := slices.IndexFunc(files, func(f log.File) bool { return f.Name == name || f.Name == name+".gz" })
			if i < 0 {
				return nil, fmt.Errorf("no log file %q (see mangal logs --list)", name)
			}
			picked = append(picked, files[i])
		}
		return picked, nil
	}
	if logsArgs.All || logsArgs.Since != "" || logsArgs.Until != "" {
		return files, nil
	}
	return slices.DeleteFunc(files, func(f log.File) bool {
		return now.Before(f.Start) || !now.Before(f.End)
	}), nil
}

var logTimeLayouts = []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04", time.DateOnly}

// parseLogTime is a duration back from now (2h, 7d) or a local date/time; a date of an
// end is the end of its day.
func parseLogTime(s string, now time.Time, end bool) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	for _, layout := range logTimeLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			if end && layout == time.DateOnly {
				t = t.AddDate(0, 0, 1).Add(-time.Nanosecond)
			}
			return t, nil
		}
	}
	d, err := parseSince(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q isn't a duration (2h, 7d) or a date/time (2026-10-12, 2026-10-12T15:04)", s)
	}
	return now.Add(-d), nil
}
