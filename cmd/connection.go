package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/remote"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/spf13/cobra"
)

var remoteArgs = struct {
	Remote string
	Local  bool
}{}

// remoteTarget is the command's backend: --remote, else the connection key unless --local.
func remoteTarget(cmd *cobra.Command) remote.Target {
	return remote.Target{Remote: remoteArgs.Remote, Local: remoteArgs.Local, Warn: remoteWarn(cmd)}
}

func remoteWarn(cmd *cobra.Command) func(string) {
	return func(msg string) { cmd.PrintErrln("warning: " + msg) }
}

// connectRemote is the client of the command's server, checked (the API version, the
// token), nil for this machine.
func connectRemote(cmd *cobra.Command) (*remote.Client, error) {
	return remote.Connect(ctxOf(cmd), remoteTarget(cmd))
}

// newServices are the command's services: this machine's core, or the connection's server.
func newServices(cmd *cobra.Command) (core.Services, error) {
	rc, err := connectRemote(cmd)
	if err != nil {
		return core.Services{}, err
	}
	if rc != nil {
		return rc.Services(), nil
	}
	c, err := newCore()
	if err != nil {
		return core.Services{}, err
	}
	return c.Services, nil
}

func init() {
	f := rootCmd.PersistentFlags()
	f.StringVar(&remoteArgs.Remote, "remote", "", "For this run, the `mangal serve` to use (a name from connections or an address), instead of the connection")
	f.BoolVar(&remoteArgs.Local, "local", false, "For this run, this machine, instead of the connection")
	rootCmd.MarkFlagsMutuallyExclusive("remote", "local")

	rootCmd.AddCommand(connectionCmd)
	connectionCmd.AddCommand(connectionLsCmd, connectionAddCmd, connectionRmCmd, connectionUseCmd, connectionStatusCmd, connectionLoginCmd, connectionLogoutCmd)
}

var connectionCmd = &cobra.Command{
	Use:     "connection",
	Aliases: []string{"remote"},
	Short:   "Pick the backend the TUI and the CLI use: this machine or a mangal serve",
	Long: `A frontend (the TUI, the CLI) has one connection: local (this machine) or a mangal serve.
"mangal connection use nas" saves it for every later run (the key connection, MANGAL_CONNECTION
for one process), --remote <name|address> and --local are for one run. mangal serve itself
ignores it. A server's API token ("mangal token add <name> --perms ..." there) is saved per
address with "mangal connection login" (with auth.keyring in the OS keyring), remote.token
(MANGAL_REMOTE_TOKEN) is sent only to the connection. Over plain HTTP to another machine the
token can be read on the way: a warning says so, use HTTPS (remote.ca_file for a private CA).`,
}

// connectionArg is the server of the command's argument (a name or an address), else the
// connection's, normalized.
func connectionArg(cmd *cobra.Command, args []string) (string, error) {
	url, err := remoteTarget(cmd).Address()
	if len(args) == 1 {
		url, err = remote.Resolve(args[0])
	}
	if err != nil {
		return "", err
	}
	if url == "" {
		return "", errors.New("that's this machine, a server's name or address is needed")
	}
	return url, nil
}

// writeConnectionKey saves a connection key, creating the config file if needed.
func writeConnectionKey(key string, value any) error {
	if err := config.CreateFile(); err != nil {
		return err
	}
	return config.WriteKey(key, value)
}

var connectionLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List the connections, the one in use marked",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		current, err := remoteTarget(cmd).Address()
		if err != nil {
			return err
		}
		mark := func(on bool) string {
			if on {
				return "*"
			}
			return " "
		}
		cmd.Printf("%s local  this machine\n", mark(current == ""))
		conns := config.Connections.Get()
		seen := false
		for _, name := range slices.Sorted(maps.Keys(conns)) {
			url, err := remote.NormalizeURL(conns[name])
			if err != nil {
				cmd.Printf("  %s  %s\n", name, err)
				continue
			}
			seen = seen || url == current
			_, from, err := remote.Token(url)
			if err != nil {
				return err
			}
			if from == "" {
				from = "no token"
			} else {
				from = "token: " + from
			}
			cmd.Printf("%s %s  %s  %s\n", mark(url == current), name, url, from)
		}
		if current != "" && !seen {
			cmd.Printf("* %s\n", current)
		}
		return nil
	},
}

var connectionAddCmd = &cobra.Command{
	Use:     "add <name> <address>",
	Short:   "Name a mangal serve",
	Example: `  mangal connection add nas https://mangal.example.com`,
	Args:    cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if err := config.ValidConnectionName(name); err != nil {
			return err
		}
		url, err := remote.NormalizeURL(args[1])
		if err != nil {
			return err
		}
		conns := maps.Clone(config.Connections.Get())
		if old, ok := conns[name]; ok && old != url {
			return fmt.Errorf("%s is %s already: mangal connection rm %s first", name, old, name)
		}
		conns[name] = url
		if err := writeConnectionKey(config.Connections.Key, conns); err != nil {
			return err
		}
		cmd.Printf("added %s: %s (mangal connection use %s switches to it)\n", name, url, name)
		return nil
	},
}

var connectionRmCmd = &cobra.Command{
	Use:   "rm <name>",
	Short: "Forget a connection's name, its saved token stays until logout",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		conns := maps.Clone(config.Connections.Get())
		if _, ok := conns[name]; !ok {
			return fmt.Errorf("no connection %q", name)
		}
		if config.Connection.Get() == name {
			return fmt.Errorf("%s is in use: mangal connection use another first", name)
		}
		delete(conns, name)
		if err := writeConnectionKey(config.Connections.Key, conns); err != nil {
			return err
		}
		cmd.Printf("removed %s\n", name)
		return nil
	},
}

var connectionUseCmd = &cobra.Command{
	Use:   "use <local|name|address>",
	Short: "Use this backend from now on, every later run too",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		value := strings.ToLower(strings.TrimSpace(args[0]))
		url, err := remote.Resolve(value)
		if err != nil {
			return err
		}
		if url != "" && strings.ContainsAny(value, ":/") {
			value = url
		}
		if err := writeConnectionKey(config.Connection.Key, value); err != nil {
			return err
		}
		cmd.Printf("now on %s\n", value)
		if name, set := config.EnvVar(config.Connection.Key); set {
			cmd.PrintErrf("warning: %s overrides it in this environment\n", name)
		}
		return nil
	},
}

var connectionLoginCmd = &cobra.Command{
	Use:   "login [name|address]",
	Short: "Save an API token of a mangal serve",
	Long: `Ask for an API token of the mangal serve (the connection by default), check it there and
save it for that address. A line on stdin when it isn't a terminal.`,
	Example: `  mangal connection login nas
  echo "$TOKEN" | mangal connection login http://nas.lan:6977`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		url, err := connectionArg(cmd, args)
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
		rc, err := remote.Open(url, token, remoteWarn(cmd))
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

var connectionLogoutCmd = &cobra.Command{
	Use:   "logout [name|address]",
	Short: "Forget the saved token of a mangal serve",
	Long:  `Forget the token saved for the address (the connection's by default); it stays valid on the server ("mangal token rm" there).`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		url, err := connectionArg(cmd, args)
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

var connectionStatusCmd = &cobra.Command{
	Use:   "status [name|address]",
	Short: "Show the backend the commands use and who the server takes the token for",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			if url, err := remoteTarget(cmd).Address(); err == nil && url == "" {
				cmd.Println("on local: the commands use this machine")
				return nil
			}
		}
		url, err := connectionArg(cmd, args)
		if err != nil {
			return err
		}
		token, from, err := remote.Token(url)
		if err != nil {
			return err
		}
		if from == "" {
			from = "none (mangal connection login)"
		}
		cmd.Printf("server: %s (%s)\ntoken: %s\n", url, remote.Name(url), from)
		rc, err := remote.Open(url, token, remoteWarn(cmd))
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
