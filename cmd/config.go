package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(configCmd)
}

var configCmd = &cobra.Command{
	Use:         "config",
	Short:       "Manage configuration",
	Args:        cobra.NoArgs,
	Annotations: map[string]string{startupAnnotation: startupConfig},
}

var configInfoArgs = struct {
	JSON  bool
	Short bool
}{}

func init() {
	configCmd.AddCommand(configInfoCmd)

	f := configInfoCmd.Flags()
	f.BoolVarP(&configInfoArgs.JSON, "json", "j", false, "JSON output")
	f.BoolVarP(&configInfoArgs.Short, "short", "s", false, "Only print 'key: value'")

	configInfoCmd.MarkFlagsMutuallyExclusive("json", "short")
}

var configInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show configuration information",
	Long:  "Show every config key with its value, default and description; secrets are masked (config get prints them).",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		type configEntry struct {
			Value       any    `json:"value"`
			Default     any    `json:"default"`
			Description string `json:"description"`
		}

		entries, err := core.NewConfig().List(ctxOf(cmd))
		if err != nil {
			return err
		}
		keys := make([]string, len(entries))
		configEntries := map[string]configEntry{}
		for i, e := range entries {
			keys[i] = e.Key
			configEntries[e.Key] = configEntry{Value: e.Value, Default: e.Default, Description: e.Description}
		}

		if configInfoArgs.JSON {
			jsonEntries, err := json.Marshal(configEntries)
			if err != nil {
				return err
			}
			cmd.Println(string(jsonEntries))
			return nil
		}

		w := styledAdaptive(cmd.OutOrStderr())
		for _, key := range keys {
			entry := configEntries[key]
			if configInfoArgs.Short {
				fmt.Fprintf(w, "%s:%s\n",
					style.Normal.Viewport.Render(key),
					style.Normal.Secondary.Render(fmt.Sprintf("%v", entry.Value)),
				)
				continue
			}
			fmt.Fprintln(w, style.Bold.Accent.Render(key))
			fmt.Fprintf(w, "  %s %s\n",
				style.Normal.Viewport.Render("Description:"),
				style.Normal.Secondary.Render(entry.Description),
			)
			fmt.Fprintf(w, "  %s %s\n",
				style.Normal.Viewport.Render("Value:"),
				style.Normal.Secondary.Render(fmt.Sprintf("%v", entry.Value)),
			)
			fmt.Fprintf(w, "  %s %s\n",
				style.Normal.Viewport.Render("Default:"),
				style.Normal.Secondary.Render(fmt.Sprintf("%v", entry.Default)),
			)
		}
		return nil
	},
}

func init() {
	configCmd.AddCommand(configWriteCmd)
}

var configWriteCmd = &cobra.Command{
	Use:   "write",
	Short: "Write configuration to disk",
	Long: `Write every config key kept in the config file to it: its values stay, the keys it doesn't
have are listed as commented defaults ("# key = value": still the default, they follow mangal's;
set one with "mangal config set" or uncomment it). Values from environment variables or flags
aren't written. The file is edited in place: its comments and order stay, retired keys are
dropped; the keys stored in the database now aren't added and their old entries are left
alone. "mangal config clean" drops the unknown and moved keys and comments out values that
are the default.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if err := config.Write(); err != nil {
			return err
		}
		successf(cmd, "wrote the config to %s", config.Path)
		return nil
	},
}

var configCleanArgs = struct {
	Apply bool
}{}

func init() {
	configCmd.AddCommand(configCleanCmd)
	configCleanCmd.Flags().BoolVar(&configCleanArgs.Apply, "apply", false, "Change the config file, instead of only listing the changes")
}

var configCleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Drop stale keys from the config file",
	Long: `Drop the keys mangal doesn't know (removed ones, typos: the closest key is suggested), the
retired ones and the ones stored in the database now (once they were copied there) from the
config file, and comment out the values that are the default
("# key = value", so they follow mangal's default). The rest of the file stays as it is,
comments included; a copy is saved next to it first (mangal.toml.<time>.bak).

Only lists the changes unless --apply is set.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		stale, err := config.StaleKeys()
		if err != nil {
			return err
		}
		if stale.Empty() && len(stale.Moved) == 0 {
			successf(cmd, "nothing to clean in %s", config.Path)
			return nil
		}
		verb := map[bool]string{false: "would ", true: ""}[configCleanArgs.Apply]
		for _, k := range stale.Unknown {
			hint := "not a mangal key"
			if k.Suggestion != "" {
				hint += fmt.Sprintf(", did you mean %q? (set it with mangal config set %s <value>)", k.Suggestion, k.Suggestion)
			}
			cmd.Printf("%sdrop %s (%s)\n", verb, k.Key, hint)
		}
		for _, k := range stale.Retired {
			cmd.Printf("%sdrop %s (no longer used: %s)\n", verb, k.Key, k.Reason)
		}
		for _, k := range stale.Moved {
			where := "stored in the database now"
			if k.NewKey != k.Key {
				where += " as " + k.NewKey
			}
			if stale.MovedKept {
				cmd.Printf("keep %s (%s, but not copied there yet: the next mangal run that opens the database does it)\n", k.Key, where)
			} else {
				cmd.Printf("%sdrop %s (%s, the entry isn't used)\n", verb, k.Key, where)
			}
		}
		if n := len(stale.Defaults); n > 0 {
			cmd.Printf("%scomment out %d %s set to the default, they follow mangal's default: %s\n", verb, n,
				map[bool]string{true: "key", false: "keys"}[n == 1], strings.Join(stale.Defaults, ", "))
		}
		if !configCleanArgs.Apply {
			cmd.Println("run with --apply to change the config file")
			return nil
		}
		backup, err := config.Clean(stale)
		if err != nil {
			return err
		}
		successf(cmd, "cleaned %s (the old file is %s)", config.Path, backup)
		return nil
	},
}

func init() {
	configCmd.AddCommand(configGetCmd)
}

var configGetCmd = &cobra.Command{
	Use:               "get key",
	Short:             "Get config value by key",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completionConfigKeys,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !config.Exists(args[0]) {
			// a provider's setting
			core.LoadProviderSettings()
		}
		value, err := config.Get(args[0])
		if err != nil {
			return err
		}
		cmd.Println(value)
		return nil
	},
}

func init() {
	configCmd.AddCommand(configSetCmd)
}

var configSetCmd = &cobra.Command{
	Use:   "set key value",
	Short: "Sets value to the config key",
	Long: `Set the value of the config key where it's stored: the config file (created if there's
none; only that key changes, the rest of the file, comments and order, stays as it is) or
the data database (the settings that are the same for every mangal using it, and the
providers' settings, providers.<id>.<key>).`,
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completionConfigKeys,
	RunE: func(cmd *cobra.Command, args []string) error {
		key, value := args[0], args[1]
		e, err := core.NewConfig().Set(ctxOf(cmd), key, value)
		if err != nil {
			return err
		}
		successf(cmd, "set %q to %v", key, config.Masked(key, value))
		if e.Source.Kind == "env" {
			cmd.PrintErrf("notice: %s overrides it in this process\n", e.Source.Name)
		}
		if mode, readable := config.ReadableByOthers(); readable && e.Secret && e.Stored == string(config.StoredFile) {
			cmd.PrintErrf("warning: %s holds secrets but others can read it (mode %o): chmod 600 it\n", config.Path, mode)
		}
		return nil
	},
}
