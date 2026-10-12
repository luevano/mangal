package cmd

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/luevano/mangal/v5/health"
	"github.com/spf13/cobra"
)

var healthArgs = struct {
	JSON bool
	Only []string
}{}

func init() {
	rootCmd.AddCommand(healthCmd)
	f := healthCmd.Flags()
	f.BoolVar(&healthArgs.JSON, "json", false, "Print the report as JSON")
	f.StringSliceVar(&healthArgs.Only, "only", nil, "Only these categories (watches, gaps, metadata, library, global) or checks (W1, G1, ...); a summary check named here (W10) lists its mangas")
}

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check the library and the watches for problems",
	Long: `Check the library and the watches for problems and say how to fix each one. Only
reads, changes nothing, makes no requests.

Watches: needing a re-link (W1), not in the library (W2), paused (W3), failing (W4), no new
chapter for 90 days (W5), provider not installed (W6), old provider id (W8), invalid
schedule (W9), library mangas not watched (W10: one line by publication status, "--only
W10" lists them), watched on two providers (W11).
Gaps: chapters missing between the first and last downloaded (G1, whole numbers only, not
up to a watch's --from or in its skip list), chapters to retry (G3).
Metadata: no AniList link (M1, with the folder tag's id), links that disagree (M3),
mangas sharing a metadata id (M6, with the one to keep).
Library: data under old provider ids (L3), mangas whose providers are all gone (L7).
Global: expired logins, Lua providers that didn't load, notifications without a Discord
webhook (A1).

With "mangal serve" running the watches are its. The exit code is 1 when something is an
error (a watch that isn't checked any more).`,
	Example: `  mangal health
  mangal health --only watches,gaps
  mangal health --only M6 --json
  mangal health --only W10`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		st := newStatus(cmd)
		defer st.Done()
		checker, err := health.New(cmd.Context())
		if err != nil {
			return err
		}
		checker.OnProgress = st.Set
		report, err := checker.Run(cmd.Context(), health.Options{Only: healthArgs.Only})
		st.Done()
		if err != nil {
			return err
		}
		for _, e := range report.Errors {
			cmd.PrintErrf("warning: %s\n", e)
		}
		if healthArgs.JSON {
			if err := printJSON(cmd.OutOrStdout(), report); err != nil {
				return err
			}
		} else {
			printHealth(cmd.OutOrStdout(), report, healthArgs.Only)
		}
		if n := report.Counts.Error; n > 0 {
			return fmt.Errorf("%s found", countNoun(n, "error", "errors"))
		}
		return nil
	},
}

// printHealth prints the findings by category: a header with the counts, each finding
// with its details and fixes, then the totals. The items of a summary finding are listed
// when only names its check.
func printHealth(w io.Writer, report health.Report, only []string) {
	for _, cat := range health.Categories {
		var of []health.Finding
		var counts health.Counts
		for _, f := range report.Findings {
			if f.Category != cat {
				continue
			}
			of = append(of, f)
			switch f.Severity {
			case health.Error:
				counts.Error++
			case health.Warning:
				counts.Warning++
			default:
				counts.Info++
			}
		}
		if len(of) == 0 {
			continue
		}
		fmt.Fprintf(w, "%s: %d (%s)\n", cat, len(of), severityCounts(counts))
		for _, f := range of {
			fmt.Fprintf(w, "  [%s] %s %s: %s\n", f.Severity, f.Check, f.Subject, f.Message)
			for _, d := range f.Details {
				fmt.Fprintf(w, "      %s\n", d)
			}
			for _, c := range f.Fix.Commands {
				fmt.Fprintf(w, "      fix: %s\n", c)
			}
			if f.Fix.Note != "" {
				fmt.Fprintf(w, "      (%s)\n", f.Fix.Note)
			}
			if !slices.ContainsFunc(only, func(o string) bool { return strings.EqualFold(strings.TrimSpace(o), f.Check) }) {
				continue
			}
			for _, item := range f.Items {
				fmt.Fprintf(w, "      %s: %s\n", item.Subject, item.Message)
				for _, c := range item.Fix.Commands {
					fmt.Fprintf(w, "          fix: %s\n", c)
				}
			}
		}
		fmt.Fprintln(w)
	}
	if report.Counts.Total() == 0 {
		fmt.Fprintln(w, "no problems found")
		return
	}
	fmt.Fprintf(w, "%s: %s\n", countNoun(report.Counts.Total(), "finding", "findings"), severityCounts(report.Counts))
	if slices.ContainsFunc(report.Findings, func(f health.Finding) bool { return slices.ContainsFunc(f.Fix.Commands, dryRunCommand) }) {
		fmt.Fprintln(w, `"mangal library bind/merge/move" and "mangal migrate" only list what they'd do, --apply does it`)
	}
}

// dryRunCommand is a command that only lists what it would do without --apply.
func dryRunCommand(c string) bool {
	for _, prefix := range []string{"mangal library bind ", "mangal library merge ", "mangal library move ", "mangal migrate"} {
		if strings.HasPrefix(c, prefix) && !strings.Contains(c, "--apply") {
			return true
		}
	}
	return false
}

func severityCounts(c health.Counts) string {
	var parts []string
	if c.Error > 0 {
		parts = append(parts, countNoun(c.Error, "error", "errors"))
	}
	if c.Warning > 0 {
		parts = append(parts, countNoun(c.Warning, "warning", "warnings"))
	}
	if c.Info > 0 {
		parts = append(parts, fmt.Sprintf("%d info", c.Info))
	}
	return strings.Join(parts, ", ")
}

func countNoun(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
