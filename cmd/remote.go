package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/remote"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/spf13/cobra"
)

var remoteArgs = struct {
	URL   string
	Local bool
}{}

// remoteFlags gives the commands --remote and --local.
func remoteFlags(cmds ...*cobra.Command) {
	for _, c := range cmds {
		c.Flags().StringVar(&remoteArgs.URL, "remote", "", "The `mangal serve` to do it on (its address), instead of remote.url or this machine's library")
		c.Flags().BoolVar(&remoteArgs.Local, "local", false, "This machine's library, even when remote.url is set")
		c.MarkFlagsMutuallyExclusive("remote", "local")
	}
}

// remoteURL is the server the command goes to (--remote, else remote.url unless --local),
// normalized, empty for this machine.
func remoteURL() (string, error) {
	raw := remoteArgs.URL
	if raw == "" && !remoteArgs.Local {
		raw = config.Remote.URL.Get()
	}
	if raw == "" {
		return "", nil
	}
	return remote.NormalizeURL(raw)
}

// remoteToken is the token for the server at url: remote.token when it's remote.url's
// (never sent to another server), else the one `mangal remote login` saved.
func remoteToken(url string) (token, from string, err error) {
	if t := config.Remote.Token.Get(); t != "" {
		if configured, err := remote.NormalizeURL(config.Remote.URL.Get()); err == nil && configured == url {
			return t, "remote.token", nil
		}
	}
	if token, err = cache.RemoteToken(url); err != nil || token == "" {
		return "", "", err
	}
	return token, "saved", nil
}

// newRemote is the client of the server at url with the token.
func newRemote(cmd *cobra.Command, url, token string) (*remote.Client, error) {
	timeout, err := time.ParseDuration(config.Remote.Timeout.Get())
	if err != nil {
		return nil, fmt.Errorf("remote.timeout: %w", err)
	}
	return remote.New(remote.Options{URL: url, Token: token, CAFile: config.Remote.CAFile.Get(), Timeout: timeout,
		Warn: func(msg string) { cmd.PrintErrln("warning: " + msg) }})
}

// connectRemote is the client of the command's server, checked (the API version, the
// token), nil for this machine.
func connectRemote(cmd *cobra.Command) (*remote.Client, error) {
	url, err := remoteURL()
	if err != nil || url == "" {
		return nil, err
	}
	token, _, err := remoteToken(url)
	if err != nil {
		return nil, err
	}
	rc, err := newRemote(cmd, url, token)
	if err != nil {
		return nil, err
	}
	if _, err := rc.Status(ctxOf(cmd)); err != nil {
		if errors.Is(err, remote.ErrUnauthorized) {
			return nil, fmt.Errorf("%s: %w (mangal remote login %s)", url, err, url)
		}
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	return rc, nil
}

// target is where a command's library changes go: this machine's core, or a server.
type target struct {
	core.Changes
	// local is nil for a server, remote nil for this machine
	local  *core.Core
	remote *remote.Client
}

// newTarget is the command's target (--remote, remote.url, --local).
func newTarget(cmd *cobra.Command) (target, error) {
	rc, err := connectRemote(cmd)
	if err != nil {
		return target{}, err
	}
	if rc != nil {
		return target{Changes: rc.Changes(), remote: rc}, nil
	}
	c, err := newCore()
	if err != nil {
		return target{}, err
	}
	return target{Changes: c.Changes, local: c}, nil
}

func (t target) ops(ctx context.Context) ([]storage.Op, error) {
	if t.remote != nil {
		return t.remote.Ops(ctx, 0)
	}
	return t.local.Library.Ops(ctx, 0)
}

func (t target) history(ctx context.Context, provider, mangaID string) ([]storage.Read, error) {
	if t.remote != nil {
		return t.remote.History(ctx, provider, mangaID)
	}
	return t.local.Library.History(ctx, provider, mangaID)
}

func (t target) watches(ctx context.Context) (watch.Manager, error) {
	if t.remote != nil {
		return t.remote.Watches(), nil
	}
	return t.local.Watches(ctx)
}

func init() {
	rootCmd.AddCommand(remoteCmd)
	remoteCmd.AddCommand(remoteLoginCmd, remoteLogoutCmd, remoteStatusCmd)
	remoteFlags(libraryRenameCmd, libraryMarkCmd, libraryMergeCmd, libraryRmCmd, libraryTrashEmptyCmd, migrateCmd,
		libraryUndoCmd, healthFixCmd, historyCmd, historyClearCmd)
}

var remoteCmd = &cobra.Command{
	Use:   "remote",
	Short: "Log in to a mangal serve on another machine",
	Long: `The library changes ("library rename", "mark", "merge", "rm", "trash empty", "migrate",
"undo"), "health fix" and "history" can run on a mangal serve elsewhere: --remote <address>,
or remote.url for every run (--local for this machine's). They send an API token of that
server ("mangal token add <name> --perms ..." there): "mangal remote login" saves it for the
address (with auth.keyring in the OS keyring), remote.token (MANGAL_REMOTE_TOKEN) is
remote.url's. Over plain HTTP to another machine the token can be read on the way: a
warning says so, use HTTPS (remote.ca_file for a private CA).`,
}

// remoteArg is the address of the command's argument, else remote.url, normalized.
func remoteArg(args []string) (string, error) {
	raw := config.Remote.URL.Get()
	if len(args) == 1 {
		raw = args[0]
	}
	if raw == "" {
		return "", errors.New("no address: give one, or set remote.url")
	}
	return remote.NormalizeURL(raw)
}

var remoteLoginCmd = &cobra.Command{
	Use:   "login [address]",
	Short: "Save an API token of a mangal serve",
	Long: `Ask for an API token of the mangal serve at the address (remote.url by default), check
it there and save it for that address. A line on stdin when it isn't a terminal.`,
	Example: `  mangal remote login https://mangal.example.com
  echo "$TOKEN" | mangal remote login http://nas.lan:6977`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		url, err := remoteArg(args)
		if err != nil {
			return err
		}
		token, err := readSecret(cmd, bufio.NewReader(cmd.InOrStdin()), "API token for "+url+": ")
		if err != nil {
			return err
		}
		if token == "" {
			return errors.New("no token: nothing saved")
		}
		rc, err := newRemote(cmd, url, token)
		if err != nil {
			return err
		}
		me, err := rc.Me(ctxOf(cmd))
		if err != nil {
			return fmt.Errorf("%s: %w", url, err)
		}
		if me.Kind != "token" {
			return fmt.Errorf("%s doesn't take the token (it's %s there): nothing saved", url, me.Kind)
		}
		if err := cache.SetRemoteToken(url, token); err != nil {
			return err
		}
		cmd.Printf("saved the token %s for %s (%s)\n", me.Name, url, strings.Join(me.Scopes, ", "))
		return nil
	},
}

var remoteLogoutCmd = &cobra.Command{
	Use:   "logout [address]",
	Short: "Forget the saved token of a mangal serve",
	Long:  `Forget the token saved for the address (remote.url by default); it stays valid on the server ("mangal token rm" there).`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		url, err := remoteArg(args)
		if err != nil {
			return err
		}
		token, err := cache.RemoteToken(url)
		if err != nil {
			return err
		}
		if token == "" {
			cmd.Printf("no token saved for %s\n", url)
			return nil
		}
		if err := cache.SetRemoteToken(url, ""); err != nil {
			return err
		}
		cmd.Printf("forgot the token of %s\n", url)
		return nil
	},
}

var remoteStatusCmd = &cobra.Command{
	Use:   "status [address]",
	Short: "Show the mangal serve the commands use and who it takes the token for",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		url, err := remoteArg(args)
		if err != nil {
			if len(args) == 0 {
				cmd.Println("no remote: remote.url isn't set, the commands use this machine's library")
				return nil
			}
			return err
		}
		token, from, err := remoteToken(url)
		if err != nil {
			return err
		}
		if from == "" {
			from = "none (mangal remote login)"
		}
		cmd.Printf("server: %s\ntoken: %s\n", url, from)
		rc, err := newRemote(cmd, url, token)
		if err != nil {
			return err
		}
		st, err := rc.Status(ctxOf(cmd))
		if err != nil {
			return fmt.Errorf("%s: %w", url, err)
		}
		version := "?"
		if st.Version != nil {
			version = *st.Version
		}
		cmd.Printf("mangal %s, API %s\n", version, st.ApiVersion)
		me, err := rc.Me(ctxOf(cmd))
		if err != nil {
			return fmt.Errorf("%s: %w", url, err)
		}
		cmd.Printf("as: %s %s (%s)\n", me.Kind, me.Name, strings.Join(me.Scopes, ", "))
		return nil
	},
}
