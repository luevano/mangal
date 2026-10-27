package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/theme/style"
	stringutil "github.com/luevano/mangal/v5/util/string"
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
			Value       any           `json:"value"`
			Default     any           `json:"default"`
			Description string        `json:"description"`
			Stored      string        `json:"stored"`
			Source      config.Source `json:"source"`
			Applies     string        `json:"applies"`
		}

		entries, err := core.NewConfig().List(ctxOf(cmd))
		if err != nil {
			return err
		}
		keys := make([]string, len(entries))
		configEntries := map[string]configEntry{}
		for i, e := range entries {
			keys[i] = e.Key
			configEntries[e.Key] = configEntry{Value: e.Value, Default: e.Default, Description: e.Description, Stored: e.Stored, Source: e.Source, Applies: e.Applies}
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
			fmt.Fprintf(w, "  %s %s\n",
				style.Normal.Viewport.Render("Stored:"),
				style.Normal.Secondary.Render(storedText(entry.Stored)+", the value comes from "+sourceText(entry.Source)+", "+appliesText(entry.Applies)),
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
		restartNotice(cmd, key)
		if mode, readable := config.ReadableByOthers(); readable && e.Secret && e.Stored == string(config.StoredFile) {
			cmd.PrintErrf("warning: %s holds secrets but others can read it (mode %o): chmod 600 it\n", config.Path, mode)
		}
		return nil
	},
}

// restartNotice says when the key is only read at start (a running serve or TUI keeps
// the old value).
func restartNotice(cmd *cobra.Command, key string) {
	if config.Applies(key) == "restart" {
		cmd.PrintErrf("notice: %s is read when mangal starts: a running mangal serve or TUI keeps the old value until it restarts\n", key)
	}
}

func storedText(stored string) string {
	if stored == string(config.StoredDB) {
		return "in the database (shared by every mangal using it)"
	}
	return "in mangal.toml (this machine)"
}

func sourceText(src config.Source) string {
	switch src.Kind {
	case "flag":
		return "the flag " + src.Name
	case "env":
		return "the environment variable " + src.Name
	case "run":
		return "this run"
	case "file":
		return "mangal.toml"
	case "db":
		return "the database"
	}
	return "the default"
}

func appliesText(applies string) string {
	switch applies {
	case "restart":
		return "a change applies when mangal restarts"
	case "reload":
		return "a change applies when the providers load again"
	}
	return "a change applies at once"
}

var configHistoryArgs = struct {
	Limit int
	JSON  bool
}{}

func init() {
	configCmd.AddCommand(configHistoryCmd)
	f := configHistoryCmd.Flags()
	f.IntVarP(&configHistoryArgs.Limit, "limit", "n", 50, "At most this many changes, 0 for all")
	f.BoolVarP(&configHistoryArgs.JSON, "json", "j", false, "JSON output")
}

var configHistoryCmd = &cobra.Command{
	Use:   "history [key]",
	Short: "List the changes of the settings stored in the database",
	Long: `List the changes of the settings stored in the database (every key, or the one given),
newest first: when, who, the key, the old and the new value (secrets masked, "default" when
unset). "mangal config undo <id>" sets a change's key back. The keys in mangal.toml have no
history.`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completionConfigKeys,
	RunE: func(cmd *cobra.Command, args []string) error {
		key := ""
		if len(args) == 1 {
			key = args[0]
		}
		changes, err := core.NewConfig().History(ctxOf(cmd), key, configHistoryArgs.Limit)
		if err != nil {
			return err
		}
		if configHistoryArgs.JSON {
			data, err := json.Marshal(changes)
			if err != nil {
				return err
			}
			cmd.Println(string(data))
			return nil
		}
		if len(changes) == 0 {
			cmd.Println("no changes")
			return nil
		}
		for _, c := range changes {
			cmd.Printf("%d  %s  %s  %s: %s -> %s\n", c.ID, c.At.Format("2006-01-02 15:04"), c.Who, c.Key, changeValue(c.Old), changeValue(c.New))
		}
		return nil
	},
}

func changeValue(v *string) string {
	if v == nil {
		return "default"
	}
	return *v
}

func init() {
	configCmd.AddCommand(configUndoCmd)
}

var configUndoCmd = &cobra.Command{
	Use:   "undo id",
	Short: "Set a setting back to its value before a change",
	Long: `Set the key of the change (its id in "mangal config history") back to its old value. It's
recorded as a change of its own, so it can be undone too. Refused when the key changed after
it (undo the later change first) and for secrets (their history doesn't keep the value).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("%q isn't a change id", args[0])
		}
		e, err := core.NewConfig().Undo(ctxOf(cmd), id)
		if err != nil {
			return err
		}
		successf(cmd, "set %q back to %v", e.Key, e.Value)
		restartNotice(cmd, e.Key)
		return nil
	},
}

var configExportArgs = struct {
	File string
}{}

func init() {
	configCmd.AddCommand(configExportCmd)
	configExportCmd.Flags().StringVarP(&configExportArgs.File, "file", "f", "", "Write to this file instead of the standard output")
}

var configExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Write the settings stored in the database as TOML",
	Long: `Write every setting stored in the database (the providers' too) with its value as TOML,
secrets left out: edit it and load it with "mangal config import". The keys in mangal.toml
aren't in it, the file itself is their copy.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		data, err := core.NewConfig().Export(ctxOf(cmd))
		if err != nil {
			return err
		}
		if configExportArgs.File == "" {
			_, err := cmd.OutOrStdout().Write(data)
			return err
		}
		if err := os.WriteFile(configExportArgs.File, data, 0o600); err != nil {
			return err
		}
		successf(cmd, "wrote the settings to %s", configExportArgs.File)
		return nil
	},
}

var configImportArgs = struct {
	Apply bool
}{}

func init() {
	configCmd.AddCommand(configImportCmd)
	configImportCmd.Flags().BoolVar(&configImportArgs.Apply, "apply", false, "Change the settings, instead of only listing the changes")
}

var configImportCmd = &cobra.Command{
	Use:   "import file",
	Short: "Load settings into the database from a TOML file",
	Long: `Load the settings of a TOML file (as "mangal config export" writes it; keys left out stay
as they are) into the database. Lists what would change; --apply changes it, as one
operation that "mangal library undo" reverts. Keys of mangal.toml, unknown keys and invalid
values are errors, nothing changes then.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := os.ReadFile(args[0])
		if err != nil {
			return err
		}
		cfg := core.NewConfig()
		changes, err := cfg.ImportPlan(ctxOf(cmd), data)
		if err != nil {
			return err
		}
		if len(changes) == 0 {
			successf(cmd, "nothing to change, the settings already are as in %s", args[0])
			return nil
		}
		verb := map[bool]string{false: "would set", true: "set"}[configImportArgs.Apply]
		for _, c := range changes {
			cmd.Printf("%s %s: %v -> %v\n", verb, c.Key, config.Masked(c.Key, c.Old), config.Masked(c.Key, c.New))
		}
		if !configImportArgs.Apply {
			cmd.Println("run with --apply to change the settings")
			return nil
		}
		id, err := cfg.Import(ctxOf(cmd), changes)
		if err != nil {
			return err
		}
		successf(cmd, "imported %s (operation %d, mangal library undo reverts it)", stringutil.Quantify(len(changes), "setting", "settings"), id)
		for _, c := range changes {
			restartNotice(cmd, c.Key)
		}
		return nil
	},
}
