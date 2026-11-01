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
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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

// sayOn tells where a command acts, before its plan or answer: a saved connection makes every
// later command act there. On stderr, stdout stays what scripts read.
func sayOn(cmd *cobra.Command, url string) { cmd.PrintErrf("on %s\n", remote.Name(url)) }

// newServices are the command's services: this machine's core, or the connection's server.
func newServices(cmd *cobra.Command) (core.Services, error) {
	rc, err := connectRemote(cmd)
	if err != nil {
		return core.Services{}, err
	}
	if rc != nil {
		sayOn(cmd, rc.URL)
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
ignores it. "mangal connection login" logs in with a user and password, the server answers with
this device's token, or saves an API token ("mangal token add <name> --perms ..." there): per
address (with auth.keyring in the OS keyring). remote.token (MANGAL_REMOTE_TOKEN) is sent only to
the connection. Over plain HTTP to another machine a password is refused, a token can be read on
the way: a warning says so, use HTTPS (remote.ca_file for a private CA).`,
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
		if err := remote.AddConnection(name, args[1]); err != nil {
			return err
		}
		url, _ := remote.Resolve(name)
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
		if err := remote.RemoveConnection(name); err != nil {
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
		if err := remote.UseConnection(args[0]); err != nil {
			return err
		}
		cmd.Printf("now on %s\n", config.Connection.Get())
		if name, set := config.EnvVar(config.Connection.Key); set {
			cmd.PrintErrf("warning: %s overrides it in this environment\n", name)
		}
		return nil
	},
}

var connectionLoginArgs = struct {
	User string
}{}

func init() {
	connectionLoginCmd.Flags().StringVarP(&connectionLoginArgs.User, "user", "u", "", "Log in as this user (asked on a terminal, empty for an API token)")
}

var connectionLoginCmd = &cobra.Command{
	Use:   "login [name|address]",
	Short: "Log in to a mangal serve, or save an API token of it",
	Long: `Log in to the mangal serve (the connection by default). With a user and password, once: the
server answers with this device's token (tui@<hostname>, the user's permissions, no tokens
permission needed; logging in again replaces it; it ends with the user's logins) and the password
isn't kept. A password is refused over plain HTTP except to this machine. With an empty user, an
API token is asked for, checked there and saved for that address. Without a terminal, stdin's line
is the token, or the password with --user.`,
	Example: `  mangal connection login nas
  mangal connection login nas --user david
  echo "$TOKEN" | mangal connection login http://nas.lan:6977`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		url, err := connectionArg(cmd, args)
		if err != nil {
			return err
		}
		in := bufio.NewReader(cmd.InOrStdin())
		user := connectionLoginArgs.User
		if _, tty := terminal(cmd.InOrStdin()); tty && user == "" {
			if user, err = readLine(cmd, in, "User on "+url+" (empty for an API token): ", false); err != nil {
				return err
			}
		}
		if user != "" {
			return loginUser(cmd, in, url, user)
		}
		token, err := readSecret(cmd, in, "API token for "+url+": ")
		if err != nil {
			return err
		}
		if token == "" {
			return errors.New("no token: nothing saved")
		}
		who, err := remote.SaveToken(ctxOf(cmd), url, token, remoteWarn(cmd))
		if err != nil {
			return err
		}
		cmd.Printf("saved the token %s for %s\n", who, url)
		return nil
	},
}

// loginUser logs the user in with the password asked for, saving this device's token.
func loginUser(cmd *cobra.Command, in *bufio.Reader, url, user string) error {
	if err := remote.CheckPasswordAddress(url); err != nil {
		return err
	}
	password, err := readSecret(cmd, in, "Password of "+user+": ")
	if err != nil {
		return err
	}
	if password == "" {
		return errors.New("no password: nothing saved")
	}
	who, err := remote.Login(ctxOf(cmd), url, user, password, remoteWarn(cmd))
	if err != nil {
		return err
	}
	cmd.Printf("logged in as %s, saved this device's token %s for %s\n", user, who, url)
	return nil
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

// How a command works with the connection.
const (
	// connFollows: on a server it goes through the server
	connFollows = "follows"
	// connThisMachine: always this machine, whatever the connection
	connThisMachine = "this machine"
	// connNotYet: refused on a server, until it's ported
	connNotYet = "not yet"
)

// commandClass is how every runnable command works with the connection, by its path without
// mangal (TestEveryCommandHasAClass). A command not on a server yet refuses: it never acts on
// this machine while the user believes they're on the server.
var commandClass = map[string]string{
	"": connFollows,

	"library rename": connFollows, "library mark": connFollows, "library merge": connFollows, "library rm": connFollows,
	"library trash": connFollows, "library trash empty": connFollows, "library undo": connFollows, "library ls": connFollows,
	"library downloads": connFollows, "library scan": connFollows, "library hold": connFollows,
	"migrate": connFollows, "history": connFollows, "history clear": connFollows, "health": connFollows, "health fix": connFollows,
	"providers up": connFollows, "logs": connFollows, "accounts": connFollows, "api": connFollows, "web": connFollows,
	"config info": connFollows, "config get": connFollows, "config set": connFollows, "config history": connFollows, "config undo": connFollows,
	"watch add": connFollows, "watch ls": connFollows, "watch rm": connFollows, "watch check": connFollows, "watch hold": connFollows,
	"watch log": connFollows, "watch move": connFollows, "watch pause": connFollows, "watch resume": connFollows, "watch set": connFollows, "watch stale": connFollows,
	"jobs": connFollows, "jobs run": connFollows, "jobs set": connFollows, "jobs pause": connFollows, "jobs resume": connFollows,

	"serve": connThisMachine, "serve token": connThisMachine, "connection ls": connThisMachine, "connection add": connThisMachine, "connection rm": connThisMachine,
	"connection use": connThisMachine, "connection status": connThisMachine, "connection login": connThisMachine, "connection logout": connThisMachine,
	"config write": connThisMachine, "config clean": connThisMachine, "config export": connThisMachine, "config import": connThisMachine,
	"path": connThisMachine, "cache clear": connThisMachine, "version": connThisMachine, "templates funcs": connThisMachine,
	"templates exec": connThisMachine, "formats": connThisMachine, "script doc": connThisMachine, "providers new": connThisMachine,
	"anilist": connThisMachine, "anilist login": connThisMachine, "kitsu": connThisMachine, "myanimelist": connThisMachine,
	"myanimelist login": connThisMachine, "myanimelist client": connThisMachine,

	"library bind": connNotYet, "library fill": connNotYet, "library retitle": connNotYet, "library urls": connNotYet, "library move": connNotYet,
	"search": connNotYet, "bind": connNotYet, "bind ls": connNotYet, "inline json": connNotYet, "inline download": connNotYet,
	"script": connNotYet, "notify webhook": connNotYet, "providers add": connNotYet, "providers ls": connNotYet, "providers rm": connNotYet,
	"user add": connNotYet, "user passwd": connNotYet, "user perms": connNotYet, "user rename": connNotYet, "user use": connNotYet,
	"user logout": connNotYet, "user rm": connNotYet, "user ls": connNotYet, "user roles": connNotYet,
	"token add": connNotYet, "token perms": connNotYet, "token ls": connNotYet, "token rm": connNotYet,
}

// commandKey is the command's entry in commandClass.
func commandKey(cmd *cobra.Command) string {
	return strings.TrimSpace(strings.TrimPrefix(cmd.CommandPath(), meta.AppName))
}

// checkConnection refuses what the command can't do on its connection, before it does
// anything; warnings are what a server's connection makes pointless.
func checkConnection(cmd *cobra.Command) (warnings []string, err error) {
	path := commandKey(cmd)
	class := commandClass[path]
	if class == connThisMachine {
		if remoteArgs.Remote != "" {
			return nil, fmt.Errorf("mangal %s always runs on this machine: --remote doesn't apply", path)
		}
		return nil, nil
	}
	url, err := remoteTarget(cmd).Address()
	// the TUI opens at the picker with this
	if err != nil && !cmd.HasParent() {
		return nil, nil
	}
	if err != nil || url == "" {
		return nil, err
	}
	name := remote.Name(url)
	if class == connNotYet {
		return nil, fmt.Errorf("mangal %s isn't on a server yet, and this is on %s: --local for this machine", path, name)
	}
	cmd.Flags().Visit(func(f *pflag.Flag) {
		if keys := f.Annotations[configKeyAnnotation]; err == nil && len(keys) == 1 && config.Side(keys[0]) != config.SideClient {
			err = fmt.Errorf("--%s: %s is %s's setting: an admin changes it there, then this runs with it", f.Name, keys[0], name)
		}
	})
	if err != nil {
		return nil, err
	}
	for _, key := range config.Keys() {
		if env, set := config.EnvVar(key); set && config.Side(key) == config.SideServer {
			warnings = append(warnings, fmt.Sprintf("%s has no effect on %s: %s is its setting", env, name, key))
		}
	}
	return warnings, nil
}
