package cmd

import (
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/cobra"
)

func TestBindFlags(t *testing.T) {
	cmd, args, err := rootCmd.Find([]string{"bind", "mango-mangapill", "mid:2", "--anilist", "30013", "--kitsu", "11", "--default", "kitsu", "--remove", "mal"})
	if err != nil || cmd != bindCmd {
		t.Fatalf("%v, %v", cmd, err)
	}
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	defer func() {
		bindArgs.Anilist, bindArgs.Kitsu, bindArgs.Default, bindArgs.Remove = "", "", "", nil
		cmd.Flags().Lookup("default").Changed = false
	}()
	if bindArgs.Anilist != "30013" || bindArgs.Kitsu != "11" || len(bindArgs.Remove) != 1 {
		t.Errorf("parsed %+v", bindArgs)
	}
	// the default isn't settable anymore
	if err := cmd.RunE(cmd, []string{"mango-mangapill", "mid:2"}); err == nil || !strings.Contains(err.Error(), "--default is gone") {
		t.Errorf("--default: %v", err)
	}
	if c, _, err := rootCmd.Find([]string{"bind", "ls"}); err != nil || c != bindLsCmd {
		t.Errorf("bind ls: %v", err)
	}
}

func TestFormatBindings(t *testing.T) {
	got := formatBindings([]storage.Binding{{Source: "kt", ID: "11", Default: true}, {Source: "al", ID: "30013"}, {Source: "mu", ID: "5"}})
	if want := "Kitsu 11 (default), AniList 30013 (backup), mu 5 (backup)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// every --json is -j too
func TestJSONFlags(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if f := c.Flags().Lookup("json"); f != nil && f.Shorthand != "j" {
			t.Errorf("%s: --json without -j", c.CommandPath())
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
	if f := inlineDownloadCmd.Flags().Lookup("json-output"); f == nil || !f.Hidden {
		t.Errorf("inline download --json-output: %+v", f)
	}
}
