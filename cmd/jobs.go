package cmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/spf13/cobra"
)

var jobsArgs = struct {
	JSON bool
}{}

func init() {
	rootCmd.AddCommand(jobsCmd)
	jobsCmd.Flags().BoolVarP(&jobsArgs.JSON, "json", "j", false, "Print JSON")
}

var jobsCmd = &cobra.Command{
	Use:   "jobs [kind]",
	Short: "The maintenance jobs: watch checks, source checks, source addresses, scan, health",
	Long: `The maintenance jobs by kind: check (the watch checks), sources (the library sources
still on their providers, health L11), urls (the source addresses), scan (the library scan)
and health (the network health checks). Each shows whether it's on or paused, its
schedule, its last run and when "mangal serve" runs it next (it runs the schedules), and how
many items a run of the missing ones would do. With a kind, its per-provider settings and
counts too.

"mangal jobs run" runs one now, "mangal jobs set" changes the settings (mangal.toml keys:
watch.schedule, health.source_check_interval, jobs.*), "mangal jobs pause|resume" stops or
restarts a kind's schedule.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		kinds, err := newKinds(nil)
		if err != nil {
			return err
		}
		ctx := ctxOf(cmd)
		if len(args) == 1 {
			kind, err := core.ParseJobKind(args[0])
			if err != nil {
				return err
			}
			k, err := kinds.Get(ctx, kind)
			if err != nil {
				return err
			}
			if jobsArgs.JSON {
				return printJSON(cmd.OutOrStdout(), k)
			}
			printJobKind(cmd.OutOrStdout(), k)
			return nil
		}
		list, err := kinds.List(ctx)
		if err != nil {
			return err
		}
		if jobsArgs.JSON {
			return printJSON(cmd.OutOrStdout(), list)
		}
		printJobKinds(cmd.OutOrStdout(), list)
		return nil
	},
}

// newKinds are the job kinds over m (nil: none, nothing runs).
func newKinds(m *jobs.Manager) (core.JobKinds, error) {
	c, err := newCore()
	if err != nil {
		return nil, err
	}
	return c.Kinds(m, core.KindsOptions{Origin: "cli"}), nil
}

// kindState is the kind's state in a word.
func kindState(k core.JobKind) string {
	switch {
	case k.Running != nil:
		return "running"
	case k.Busy != "":
		return "busy"
	case k.Manual:
		return "manual"
	case !k.Enabled:
		return "off"
	case k.Paused:
		return "paused"
	}
	return "on"
}

// lastRun is the kind's last run in a line.
func lastRun(k core.JobKind) string {
	if k.Last == nil {
		return "never"
	}
	at := k.Last.Finished
	if at.IsZero() {
		at = k.Last.Started
	}
	what := k.Last.Summary
	if k.Last.Error != "" {
		what = "failed: " + k.Last.Error
	}
	if what == "" {
		return at.Local().Format(time.DateTime)
	}
	return at.Local().Format(time.DateTime) + " " + firstLine(what, 60)
}

func nextRun(k core.JobKind) string {
	if k.Next.IsZero() {
		return k.NoNext
	}
	return k.Next.Local().Format(time.DateTime)
}

func printJobKinds(out io.Writer, list []core.JobKind) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "KIND\tSTATE\tSCHEDULE\tMISSING\tLAST\tNEXT")
	for _, k := range list {
		missing := "-"
		if k.Missing != nil {
			missing = fmt.Sprint(*k.Missing)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", k.Kind, kindState(k), cmp.Or(k.Schedule, "-"), missing, lastRun(k), nextRun(k))
	}
	_ = tw.Flush()
}

func printJobKind(out io.Writer, k core.JobKind) {
	fmt.Fprintf(out, "%s (%s): %s\n", k.Title, k.Kind, k.About)
	fmt.Fprintf(out, "state: %s\n", kindState(k))
	if k.ScheduleKey != "" {
		fmt.Fprintf(out, "schedule: %s (%s)\n", k.Schedule, k.ScheduleKey)
	}
	fmt.Fprintf(out, "last: %s\nnext: %s\n", lastRun(k), nextRun(k))
	if k.Running != nil {
		fmt.Fprintf(out, "running: job %d %s\n", k.Running.ID, k.Running.Message)
	}
	if k.Busy != "" {
		fmt.Fprintf(out, "busy: %s\n", k.Busy)
	}
	if k.Missing != nil {
		fmt.Fprintf(out, "missing: %d %s\n", *k.Missing, k.MissingWhat)
	}
	if len(k.Providers) == 0 {
		return
	}
	fmt.Fprintf(out, "\nproviders (%s):\n", k.ProvidersKey)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PROVIDER\tON\tINTERVAL\tITEMS\tMISSING\tNEXT")
	for _, p := range k.Providers {
		on := "on"
		if !p.On {
			on = "off"
		}
		if p.Default != "" {
			on += " (default " + p.Default + ")"
		}
		next := "-"
		if !p.Next.IsZero() {
			next = p.Next.Local().Format(time.DateTime)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%s\n", p.Provider, on, cmp.Or(p.Interval, "-"), p.Items, p.Missing, next)
	}
	_ = tw.Flush()
}

var jobsRunArgs = struct {
	All bool
}{}

func init() {
	jobsCmd.AddCommand(jobsRunCmd)
	jobsRunCmd.Flags().BoolVar(&jobsRunArgs.All, "all", false, "Every item, not only the missing ones")
}

var jobsRunCmd = &cobra.Command{
	Use:   "run <kind>",
	Short: "Run a maintenance job now",
	Long: `Run a kind of maintenance job now, here, and wait for it: only the missing items (check:
the due watches; sources: the sources not checked within their interval; urls: the sources
without an address not asked within it), or --all of them; scan and health always do
everything. The providers off for the kind (jobs.<kind>.providers) are skipped.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		kind, err := core.ParseJobKind(args[0])
		if err != nil {
			return err
		}
		st := newStatus(cmd)
		defer st.Done()
		ctx := changeCtx(cmd, st)
		m := jobs.New(ctx)
		defer m.Shutdown(10 * time.Second)
		kinds, err := newKinds(m)
		if err != nil {
			return err
		}
		job, err := kinds.Run(ctx, kind, jobsRunArgs.All)
		if errors.Is(err, core.ErrNothingToDo) {
			st.Clear()
			cmd.Println(err)
			return nil
		}
		if err != nil {
			return err
		}
		j := runJob(ctx, m, job.ID, st.Set)
		st.Clear()
		switch {
		case j.Err != nil:
			return fmt.Errorf("%s: %w", j.Title, j.Err)
		case j.Status == jobs.Cancelled:
			return fmt.Errorf("%s: %w", j.Title, context.Canceled)
		}
		cmd.Printf("%s: %s\n", j.Title, j.Message)
		return nil
	},
}

// runJob waits for the job, telling set what it does.
func runJob(ctx context.Context, m *jobs.Manager, id int64, set func(string)) jobs.Job {
	// the job stops with ctx (the manager's): its end is what's reported
	done := ctx.Done()
	for {
		changed := m.Changed()
		j, ok := m.Get(id)
		if !ok || !j.Status.Active() {
			return j
		}
		msg := j.Message
		if j.Total > 0 {
			msg = fmt.Sprintf("%s (%d/%d)", msg, j.Done, j.Total)
		}
		set(msg)
		select {
		case <-changed:
		case <-done:
			done = nil
		}
	}
}

var jobsSetArgs = struct {
	Enabled   bool
	Schedule  string
	Providers []string
	Delay     string
}{}

func init() {
	jobsCmd.AddCommand(jobsSetCmd)
	f := jobsSetCmd.Flags()
	f.BoolVar(&jobsSetArgs.Enabled, "enabled", true, "Whether mangal serve runs it on its schedule")
	f.StringVar(&jobsSetArgs.Schedule, "schedule", "", "Its schedule: a cron schedule (check) or a Go duration (sources, urls; 0 off)")
	f.StringArrayVar(&jobsSetArgs.Providers, "provider", nil, `A provider's setting, id=on|off|<interval> (a cron schedule for check), id= back to the default; repeatable`)
	f.StringVar(&jobsSetArgs.Delay, "delay", "", "The pause between two requests to a provider (every kind): min-max, e.g. 1s-3s")
}

var jobsSetCmd = &cobra.Command{
	Use:   "set <kind>",
	Short: "Change a maintenance job's settings",
	Long: `Change a kind's settings in the config file: --enabled, --schedule, a provider's setting
(--provider mango-mangadex=off, =on, =336h; MangaPlus is off for sources, urls and health
unless set on), and the pause between requests (--delay 1s-3s, shared by the kinds).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		kind, err := core.ParseJobKind(args[0])
		if err != nil {
			return err
		}
		var s core.JobSettings
		f := cmd.Flags()
		if f.Changed("enabled") {
			s.Enabled = &jobsSetArgs.Enabled
		}
		if f.Changed("schedule") {
			s.Schedule = &jobsSetArgs.Schedule
		}
		for _, p := range jobsSetArgs.Providers {
			id, v, ok := strings.Cut(p, "=")
			if !ok || strings.TrimSpace(id) == "" {
				return fmt.Errorf("--provider %q: want id=on, off or an interval", p)
			}
			if s.Providers == nil {
				s.Providers = map[string]string{}
			}
			s.Providers[strings.TrimSpace(id)] = v
		}
		if f.Changed("delay") {
			lo, hi, ok := strings.Cut(jobsSetArgs.Delay, "-")
			if !ok {
				hi = lo
			}
			lo, hi = strings.TrimSpace(lo), strings.TrimSpace(hi)
			s.DelayMin, s.DelayMax = &lo, &hi
		}
		if s.Enabled == nil && s.Schedule == nil && s.Providers == nil && s.DelayMin == nil {
			return errors.New("nothing to change, see --help")
		}
		kinds, err := newKinds(nil)
		if err != nil {
			return err
		}
		k, err := kinds.Set(ctxOf(cmd), kind, s)
		if err != nil {
			return err
		}
		printJobKind(cmd.OutOrStdout(), k)
		return nil
	},
}

func init() {
	for _, paused := range []bool{true, false} {
		use, short := "resume <kind>", "Resume a maintenance job's schedule"
		if paused {
			use, short = "pause <kind>", "Pause a maintenance job's schedule (a running one goes on)"
		}
		jobsCmd.AddCommand(&cobra.Command{
			Use:   use,
			Short: short,
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				kind, err := core.ParseJobKind(args[0])
				if err != nil {
					return err
				}
				kinds, err := newKinds(nil)
				if err != nil {
					return err
				}
				k, err := kinds.Pause(ctxOf(cmd), kind, paused)
				if err != nil {
					return err
				}
				cmd.Printf("%s: %s, next %s\n", k.Title, kindState(k), nextRun(k))
				return nil
			},
		})
	}
}
