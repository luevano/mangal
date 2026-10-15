package cmd

import (
	"testing"

	"github.com/luevano/mangal/v5/core"
	"github.com/spf13/cobra"
)

// every command is a feature's (core.Features) or a mode's own, every feature's command
// is there
func TestCommandsAreFeatures(t *testing.T) {
	commands := map[string]bool{}
	var walk func(c *cobra.Command, path string)
	walk = func(c *cobra.Command, path string) {
		for _, sub := range c.Commands() {
			p := sub.Name()
			if path != "" {
				p = path + " " + p
			}
			commands[p] = true
			if sub.Runnable() && !sub.Hidden && len(core.FeaturesOf(p)) == 0 && !core.IsModeCommand(p) {
				t.Errorf("command %q is no feature's (core/features.go)", p)
			}
			walk(sub, p)
		}
	}
	walk(rootCmd, "")
	for _, f := range core.Features {
		for _, c := range f.CLI {
			if !commands[c] {
				t.Errorf("%s: no command %q", f.Name, c)
			}
		}
	}
}
