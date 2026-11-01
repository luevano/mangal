package cmd

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/config"
	"github.com/spf13/cobra"
)

// runnable are the commands that run, without cobra's own.
func runnable(c *cobra.Command) []*cobra.Command {
	var all []*cobra.Command
	if c.Runnable() && c.Name() != "help" && startup(c) != startupNone || c == versionCmd {
		all = append(all, c)
	}
	for _, sub := range c.Commands() {
		all = append(all, runnable(sub)...)
	}
	return all
}

// A new command says how it works with the connection, so none acts on this machine while
// the user is on a server.
func TestEveryCommandHasAClass(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range runnable(rootCmd) {
		key := commandKey(c)
		seen[key] = true
		_, classed := commandClass[key]
		_, refused := serverRefuses[key]
		if classed == refused {
			t.Errorf("%q needs to be in commandClass or serverRefuses, one of them", key)
		}
	}
	for _, key := range slices.Concat(slices.Sorted(maps.Keys(commandClass)), slices.Sorted(maps.Keys(serverRefuses))) {
		if !seen[key] {
			t.Errorf("%q is no command", key)
		}
	}
}

// resetConnection puts the connection keys back after the test.
func resetConnection(t *testing.T) {
	t.Cleanup(func() {
		_ = config.Set(config.Connection.Key, config.LocalConnection)
		_ = config.Set(config.Connections.Key, map[string]string{})
	})
}

func onServer(t *testing.T) {
	t.Helper()
	resetConnection(t)
	if err := config.Set(config.Connections.Key, map[string]string{"nas": "http://nas.lan:6977"}); err != nil {
		t.Fatal(err)
	}
	if err := config.Set(config.Connection.Key, "nas"); err != nil {
		t.Fatal(err)
	}
}

func TestRefusedOnAServer(t *testing.T) {
	onServer(t)
	for _, c := range []*cobra.Command{notifyWebhookCmd, providersAddCmd, providersRmCmd} {
		if _, err := checkConnection(c); err == nil || !strings.Contains(err.Error(), "--local") || !strings.Contains(err.Error(), "machine") {
			t.Errorf("%s: %v", c.Name(), err)
		}
	}
	if _, err := checkConnection(libraryRmCmd); err != nil {
		t.Errorf("library rm: %v", err)
	}
	if _, err := checkConnection(serveCmd); err != nil {
		t.Errorf("serve reads the connection: %v", err)
	}
	remoteArgs.Local = true
	t.Cleanup(func() { remoteArgs.Local = false })
	if _, err := checkConnection(notifyWebhookCmd); err != nil {
		t.Errorf("notify webhook --local: %v", err)
	}
}

// serve never reads the connection, a bad one neither.
func TestServeIgnoresTheConnection(t *testing.T) {
	resetConnection(t)
	if err := config.Set(config.Connection.Key, "typo"); err != nil {
		t.Fatal(err)
	}
	if _, err := checkConnection(serveCmd); err != nil {
		t.Error(err)
	}
	if err := config.Validate(); err != nil {
		t.Error(err)
	}
	if _, err := checkConnection(libraryLsCmd); err == nil {
		t.Error("a frontend gets past a connection that doesn't exist")
	}
	remoteArgs.Remote = "http://x.lan"
	t.Cleanup(func() { remoteArgs.Remote = "" })
	if _, err := checkConnection(serveCmd); err == nil {
		t.Error("serve takes --remote")
	}
}

func TestOverridingFlagRefusedOnAServer(t *testing.T) {
	onServer(t)
	c := &cobra.Command{Use: "x"}
	rootCmd.AddCommand(c)
	t.Cleanup(func() { rootCmd.RemoveCommand(c) })
	commandClass["x"] = connFollows
	t.Cleanup(func() { delete(commandClass, "x") })
	c.Flags().String("format", "", "")
	configKeyFlag(c.Flags(), "format", config.Download.Format.Key)
	if _, err := checkConnection(c); err != nil {
		t.Errorf("not given: %v", err)
	}
	if err := c.Flags().Set("format", "pdf"); err != nil {
		t.Fatal(err)
	}
	if _, err := checkConnection(c); err == nil || !strings.Contains(err.Error(), "download.format is nas's") {
		t.Errorf("given: %v", err)
	}
	remoteArgs.Local = true
	t.Cleanup(func() { remoteArgs.Local = false })
	if _, err := checkConnection(c); err != nil {
		t.Errorf("local: %v", err)
	}
}
