package cmd

import (
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/health"
	"github.com/spf13/cobra"
)

var healthFixArgs = struct {
	Choice  string
	Apply   bool
	Network bool
}{}

func init() {
	healthCmd.AddCommand(healthFixCmd)
	f := healthFixCmd.Flags()
	f.StringVar(&healthFixArgs.Choice, "choice", "", "The id to link when the fix offers several, the library manga to keep of a merge")
	f.BoolVar(&healthFixArgs.Apply, "apply", false, "Fix it, instead of only listing what would happen")
	f.BoolVar(&healthFixArgs.Network, "network", false, "Run the finding's check with the network checks (W12, G2, A2, M1's AniList search)")
}

var healthFixCmd = &cobra.Command{
	Use:   "fix <key>",
	Short: "Fix a finding as the TUI's health screen does",
	Long: `Fix the finding with the key (from "mangal health") with the change its fix makes, as the
TUI's health screen does: link an id (--choice when it offers several), merge duplicates
(--choice the library manga to keep, the suggested one by default), mark folders, forget
a manga whose folder is gone, rename folders (with the cached metadata), empty the trash
(can't be undone), resume a watch or watch a manga. A scan has no plan: "mangal library
scan" does it.

Its check runs again first (with --network for the network checks), so the fix is of the
finding as it is now. Library changes can be undone ("mangal library undo"). Only lists
what would happen unless --apply is set.`,
	Example: `  mangal health fix L2/manga/12
  mangal health fix M1/manga/5 --network --choice 74347 --apply
  mangal health fix M6/manga/3 --choice 4 --apply`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newCore()
		if err != nil {
			return err
		}
		st := newStatus(cmd)
		defer st.Done()
		ctx := changeCtx(cmd, st)
		st.Set("checking")
		f, err := c.Finding(ctx, args[0], healthFixArgs.Network)
		if err != nil {
			return err
		}
		plan, err := c.Changes.Plan(ctx, core.HealthFixReq{Finding: f, Choice: healthFixArgs.Choice})
		st.Clear()
		if err != nil {
			return err
		}
		fix := plan.Detail.(health.FixResult)
		cmd.Printf("%s: %s\n", findingTitle(f), f.Message)
		if plan.Nothing {
			cmd.Println(fix.Message)
			return nil
		}
		printLines(cmd, plan.Lines)
		for _, w := range plan.Warnings {
			cmd.Println("warning: " + w)
		}
		if plan.Irreversible {
			cmd.Println("it can't be undone")
		}
		if !healthFixArgs.Apply {
			cmd.Println("run with --apply to fix it")
			return nil
		}
		res, err := c.Changes.ApplyPlan(ctx, plan, applyOpts(st))
		st.Done()
		undoHint(cmd, res.Snapshot, err)
		if err != nil {
			return err
		}
		if done, ok := res.Detail.(health.FixResult); ok {
			cmd.Println(done.Message)
		}
		return nil
	},
}
