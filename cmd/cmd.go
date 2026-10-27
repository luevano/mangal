package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	cc "github.com/ivanpirog/coloredcobra"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/tui"
	"github.com/luevano/mangal/v5/util"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const groupMode = "mode"

// startupAnnotation says what a command needs from the startup (setup), the default is
// everything.
const startupAnnotation = "mangal.startup"

const (
	// startupNone: nothing, the config is loaded but not checked (version, help, completion)
	startupNone = "none"
	// startupConfig: the log and icons, not the storage, and an invalid config doesn't stop
	// it (the config commands, so they can fix it)
	startupConfig = "config"
	// startupNoStorage: everything but the storage
	startupNoStorage = "nostorage"
)

// configKeyAnnotation on a flag is the config key it sets when given.
const configKeyAnnotation = "mangal.config_key"

var rootArgs = struct {
	Config  string
	Debug   bool
	Verbose bool
}{}

// loadErr is the error loading the config, before the command runs.
var loadErr error

var rootCmd = &cobra.Command{
	Use:               meta.AppName,
	Long:              fmt.Sprintf("%s, CLI manga downloader", meta.AppName),
	Args:              cobra.NoArgs,
	SilenceErrors:     true,
	SilenceUsage:      true,
	PersistentPreRunE: setup,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return tui.Run(cmd.Context())
	},
}

func init() {
	f := rootCmd.PersistentFlags()
	f.StringVar(&rootArgs.Config, "config", config.Path, "Config file path")
	f.BoolVar(&rootArgs.Debug, "debug", false, "Log debug messages (every request and page downloaded)")
	f.BoolVarP(&rootArgs.Verbose, "verbose", "v", false, "Print what long commands are doing as lines on stderr (a terminal shows one changing line without it)")

	rootCmd.AddGroup(&cobra.Group{
		ID:    groupMode,
		Title: "Mode Commands:",
	})
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return fmt.Errorf("%s (see %s --help)", err, cmd.CommandPath())
	})
}

func Execute() {
	noFileCompletion(rootCmd)
	rootCmd.SetOut(os.Stdout)
	rootCmd.SetErr(os.Stderr)
	rootCmd.SetIn(os.Stdin)

	// the config is needed before cobra runs, for the colored help
	loadErr = config.Load(configFlag(os.Args[1:]))
	if loadErr == nil && config.CLI.ColoredHelp.Get() {
		cc.Init(&cc.Config{
			RootCmd:         rootCmd,
			Headings:        cc.HiCyan + cc.Bold + cc.Underline,
			Commands:        cc.HiYellow + cc.Bold,
			Example:         cc.Italic,
			ExecName:        cc.Bold,
			Flags:           cc.Bold,
			FlagsDataType:   cc.Italic + cc.HiBlue,
			Aliases:         cc.Italic,
			NoExtraNewlines: true,
			NoBottomNewline: true,
		})
	}

	// cancelled on ctrl+c, commands get it with cmd.Context()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// a second ctrl+c kills mangal if something doesn't stop
	context.AfterFunc(ctx, stop)
	// close the providers (browsers, flaresolverr sessions) and databases however mangal exits
	util.OnExit(func() {
		if err := errors.Join(client.Shutdown(), cache.Close()); err != nil {
			log.Error("close on exit: %s", err)
		}
		_ = log.Close()
	})

	err := rootCmd.ExecuteContext(ctx)
	if err != nil {
		fmt.Fprintf(styled(rootCmd.ErrOrStderr()), "%s %s\n", icon.Cross.Colored(), util.ErrorTree(err))
	}
	util.RunExitHooks()
	stop()
	if err != nil {
		os.Exit(1)
	}
}

// configFlag finds --config in the arguments, before cobra parses them.
func configFlag(args []string) string {
	fs := pflag.NewFlagSet("", pflag.ContinueOnError)
	fs.ParseErrorsAllowlist.UnknownFlags = true
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	path := fs.String("config", "", "")
	fs.BoolP("help", "h", false, "")
	_ = fs.Parse(args)
	if *path == "" {
		return os.Getenv(globalEnv["config"])
	}
	return *path
}

// globalEnv are the variables of the global flags that aren't config keys (the flags
// that are have their key's, see configKeyFlag). The flags of one command (--apply,
// --yes, --json, --query, ...) never get one: variables set how the app behaves, not single
// actions, and MANGAL_APPLY in a profile would turn every plan into an action.
var globalEnv = map[string]string{"config": "MANGAL_CONFIG", "debug": "MANGAL_DEBUG", "verbose": "MANGAL_VERBOSE"}

// applyGlobalEnv sets the global flags not given from their variables.
func applyGlobalEnv(fs *pflag.FlagSet) error {
	for name, env := range globalEnv {
		f := fs.Lookup(name)
		if f == nil || f.Changed {
			continue
		}
		if v, ok := os.LookupEnv(env); ok {
			if err := f.Value.Set(v); err != nil {
				return fmt.Errorf("%s: %w", env, err)
			}
		}
	}
	return nil
}

func startup(cmd *cobra.Command) string {
	switch cmd.Name() {
	case "help", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return startupNone
	}
	for c := cmd; c != nil; c = c.Parent() {
		if s, ok := c.Annotations[startupAnnotation]; ok {
			return s
		}
		// cobra's
		if c.Name() == "completion" && c.HasParent() && !c.Parent().HasParent() {
			return startupNone
		}
	}
	return ""
}

// setup runs before every command: validate the config (with the flags now), start the
// log, set the icons and open the storage, as far as the command needs them.
func setup(cmd *cobra.Command, _ []string) error {
	level := startup(cmd)
	if level == startupNone {
		return nil
	}
	if loadErr != nil {
		return loadErr
	}
	if err := applyGlobalEnv(cmd.Flags()); err != nil {
		return err
	}
	if err := bindFlags(cmd.Flags()); err != nil {
		return err
	}
	config.Who = cmd.CommandPath()
	if !cmd.HasParent() {
		config.Who = "tui"
	}
	// told once the log is open (it's configured by the same config)
	var warnings []string
	for _, key := range config.Unsupported() {
		hint := ""
		if s := config.Suggest(key); s != "" {
			hint = fmt.Sprintf(" (did you mean %q?)", s)
		}
		warnings = append(warnings, fmt.Sprintf("config key %q is not supported%s, mangal config clean drops it", key, hint))
	}
	for _, key := range config.Retired() {
		warnings = append(warnings, fmt.Sprintf("config key %q is no longer used (%s), mangal config clean drops it", key, config.RetiredReason(key)))
	}
	for _, env := range config.UnknownEnv() {
		hint := ""
		if env.Suggestion != "" {
			hint = fmt.Sprintf(" (did you mean %s?)", env.Suggestion)
		}
		warnings = append(warnings, fmt.Sprintf("environment variable %s sets no config key%s", env.Key, hint))
	}
	if err := config.Validate(); err != nil && level != startupConfig {
		// no log without a valid config: they may say why
		for _, w := range warnings {
			cmd.PrintErrf("warning: %s\n", w)
		}
		return err
	}

	// validated by the config, Info otherwise
	logLevel, _ := log.ParseLevel(config.Log.Level.Get())
	if rootArgs.Debug {
		logLevel = log.LevelDebug
	}
	// validated by the config, Daily otherwise
	rotate, _ := log.ParsePeriod(config.Log.Rotate.Get())
	log.Init(log.Options{
		Dir:          path.Logs(),
		Level:        logLevel,
		Rotate:       rotate,
		MaxSize:      config.Log.MaxSize.Get(),
		Keep:         config.Log.KeepDays.Get(),
		MaxFiles:     config.Log.MaxFiles.Get(),
		MaxTotalSize: config.Log.MaxTotalSize.Get(),
		Compress:     config.Log.Compress.Get(),
	})
	icon.SetType(config.Icons.Get())
	// serve's journal is its stderr: the same lines as the log file from here on, the
	// database upgrades and migrations below included (serve.Run keeps it)
	if cmd == serveCmd {
		log.SetOutput(cmd.ErrOrStderr())
	}
	for _, w := range warnings {
		log.Warn("%s", w)
		if cmd != serveCmd {
			cmd.PrintErrf("warning: %s\n", w)
		}
	}

	// the database keys are read from the data database: the commands that work without
	// the storage only warn when it can't be opened
	imported, err := openStorage(cmd)
	if level == startupConfig || level == startupNoStorage {
		if err != nil {
			cmd.PrintErrf("warning: %s, the settings stored in the database aren't read\n", err)
		} else if imported != "" {
			cmd.PrintErrf("notice: %s\n", imported)
		}
		return nil
	}
	if err != nil {
		return err
	}
	for _, notice := range append(cache.Notices(), anilist.LoginNotice()) {
		if notice == "" {
			continue
		}
		// in the log file too; serve's log is its stderr already, the TUI shows them itself
		log.Info("notice: %s", notice)
		if cmd.HasParent() && cmd != serveCmd {
			cmd.PrintErrf("notice: %s\n", notice)
		}
	}
	return nil
}

// openStorage opens the databases and copies mangal.toml's moved keys into the data
// database the first time (client.ImportSettings): imported says so, it's in the notices
// with the one about the moved keys left in the file.
func openStorage(cmd *cobra.Command) (imported string, err error) {
	if err := cache.Open(); err != nil {
		return "", fmt.Errorf("open storage: %w", err)
	}
	imported, err = client.ImportSettings(cmd.Context())
	if err != nil {
		return "", err
	}
	users, err := client.EnsureUsers()
	if err != nil {
		return "", err
	}
	for _, n := range []string{imported, client.MovedNotice(), strings.TrimSpace(users)} {
		if n != "" {
			cache.AddNotice(n)
		}
	}
	return imported, nil
}

// noFileCompletion makes the arguments of the commands without a completion complete
// nothing: none takes a local file.
func noFileCompletion(c *cobra.Command) {
	if c.ValidArgsFunction == nil && len(c.ValidArgs) == 0 {
		c.ValidArgsFunction = cobra.NoFileCompletions
	}
	for _, sub := range c.Commands() {
		noFileCompletion(sub)
	}
}

// bindFlags binds the flags that set a config key (configKeyFlag) to it.
func bindFlags(fs *pflag.FlagSet) error {
	var errs []error
	fs.VisitAll(func(f *pflag.Flag) {
		if keys := f.Annotations[configKeyAnnotation]; len(keys) == 1 {
			errs = append(errs, config.BindPFlag(keys[0], f))
		}
	})
	return errors.Join(errs...)
}

// configKeyFlag marks the flag as setting the config key when given, the config applies
// otherwise.
func configKeyFlag(fs *pflag.FlagSet, name, key string) {
	f := fs.Lookup(name)
	f.Usage += fmt.Sprintf(" (default: config %s)", key)
	if f.Annotations == nil {
		f.Annotations = map[string][]string{}
	}
	f.Annotations[configKeyAnnotation] = []string{key}
}
