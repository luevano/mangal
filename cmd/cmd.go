package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
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
	f.BoolVar(&rootArgs.Debug, "debug", false, "Log debug messages (e.g. every page downloaded)")
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
	go func() {
		<-ctx.Done()
		stop()
	}()
	// close the providers (browsers, flaresolverr sessions) and databases however mangal exits
	util.OnExit(func() {
		if err := errors.Join(client.Shutdown(), cache.Close()); err != nil {
			log.Error("close on exit: %s", err)
		}
		_ = log.Close()
	})

	err := rootCmd.ExecuteContext(ctx)
	if err != nil {
		rootCmd.PrintErrf("%s %s\n", icon.Cross.Colored(), util.ErrorTree(err))
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
	return *path
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
	if err := bindFlags(cmd.Flags()); err != nil {
		return err
	}
	for _, key := range config.Unsupported() {
		cmd.PrintErrf("warning: config key %q is not supported, update the config file\n", key)
	}
	if err := config.Validate(); err != nil && level != startupConfig {
		return err
	}

	logLevel := log.LevelInfo
	if rootArgs.Debug {
		logLevel = log.LevelDebug
	}
	log.Init(log.Options{Dir: path.Logs(), Level: logLevel})
	icon.SetType(config.Icons.Get())

	if level == startupConfig || level == startupNoStorage {
		return nil
	}
	if err := cache.Open(); err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	// the TUI shows them itself
	if !cmd.HasParent() {
		return nil
	}
	for _, notice := range append(cache.Notices(), anilist.LoginNotice()) {
		if notice != "" {
			cmd.PrintErrf("notice: %s\n", notice)
		}
	}
	return nil
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
