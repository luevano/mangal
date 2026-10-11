package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	cc "github.com/ivanpirog/coloredcobra"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/tui"
	"github.com/luevano/mangal/v5/util"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const groupMode = "mode"

var rootCmd = &cobra.Command{
	Use:  meta.AppName,
	Long: fmt.Sprintf("%s, CLI manga downloader", meta.AppName),
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		if err := tui.Run(); err != nil {
			errorf(cmd, "%s", util.ErrorTree(err))
		}
	},
	// A default completion option is always added, this would disable it.
	// CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true}
}

func Execute() {
	rootCmd.SetOut(os.Stdout)
	rootCmd.SetErr(os.Stderr)
	rootCmd.SetIn(os.Stdin)

	rootCmd.AddGroup(&cobra.Group{
		ID:    groupMode,
		Title: "Mode Commands:",
	})

	// Load the config before any command execution
	// Looks weird, it sets the same variable it uses for default and then path.ConfigDir
	// will also read this set flag but it also creates the directory if doesn't exist.
	rootCmd.PersistentFlags().StringVar(&config.Path, "config", config.Path, "Config file path")
	cobra.OnInitialize(initConfig(rootCmd.PersistentFlags().Lookup("config")))

	if config.CLI.ColoredHelp.Get() {
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
	defer stop()
	// close the providers (browsers, flaresolverr sessions) and databases however mangal exits
	util.OnExit(func() {
		if err := errors.Join(client.CloseAll(), cache.Close()); err != nil {
			log.Log("close on exit: %s", err)
		}
	})
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		errorf(rootCmd, "%s", util.ErrorTree(err))
	}
	util.RunExitHooks()
}

// re-reads the config from the changed flag
func initConfig(flag *pflag.Flag) func() {
	return func() {
		if flag.Changed {
			if err := config.Load(config.Path); err != nil {
				errorf(rootCmd, "%s", util.ErrorTree(fmt.Errorf("load config %q: %w", config.Path, err)))
			}
		} else if err := config.LoadError(); err != nil {
			errorf(rootCmd, "%s", util.ErrorTree(fmt.Errorf("load config %q: %w", config.Path, err)))
		}
		// once the config finishes initializing (using custom config path or not), set the actual icon type
		icon.SetType(config.Icons.Get())

		if err := cache.Open(); err != nil {
			fmt.Fprintf(os.Stderr, "%s storage: %s\n", icon.Cross.Colored(), err)
			util.Exit(1)
		}
		for _, notice := range append(cache.Notices(), anilist.LoginNotice()) {
			if notice == "" {
				continue
			}
			fmt.Fprintf(os.Stderr, "notice: %s\n", notice)
		}
	}
}
