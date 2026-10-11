package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/luevano/mangal/v5/config"
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
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		type configEntry struct {
			Value       any    `json:"value"`
			Default     any    `json:"default"`
			Description string `json:"description"`
		}

		keys := config.Keys()
		configEntries := map[string]configEntry{}
		for _, key := range keys {
			value, err := config.Get(key)
			if err != nil {
				return err
			}
			def, _ := config.Default(key)
			description, _ := config.Description(key)
			configEntries[key] = configEntry{
				Value:       value,
				Default:     def,
				Description: description,
			}
		}

		if configInfoArgs.JSON {
			jsonEntries, err := json.Marshal(configEntries)
			if err != nil {
				return err
			}
			cmd.Println(string(jsonEntries))
			return nil
		}

		for _, key := range keys {
			entry := configEntries[key]
			if configInfoArgs.Short {
				cmd.Printf("%s:%s\n",
					style.Normal.Viewport.Render(key),
					style.Normal.Secondary.Render(fmt.Sprintf("%v", entry.Value)),
				)
				continue
			}
			cmd.Println(style.Bold.Accent.Render(key))
			cmd.Printf("  %s %s\n",
				style.Normal.Viewport.Render("Description:"),
				style.Normal.Secondary.Render(entry.Description),
			)
			cmd.Printf("  %s %s\n",
				style.Normal.Viewport.Render("Value:"),
				style.Normal.Secondary.Render(fmt.Sprintf("%v", entry.Value)),
			)
			cmd.Printf("  %s %s\n",
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
	Long: `Write every config key to the config file: its current values and the defaults of the
keys it doesn't have (values from environment variables or flags aren't written). The file is
re-serialised, comments and order are lost.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if err := config.Write(); err != nil {
			return err
		}
		successf(cmd, "Wrote config to %s", config.Path)
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
	Long: `Set the value of the config key in the config file (created if there's none). Only that
key changes, but the file is re-serialised: comments and order are lost.`,
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completionConfigKeys,
	RunE: func(cmd *cobra.Command, args []string) error {
		key, value := args[0], args[1]
		raw, err := config.Parse(key, value)
		if err != nil {
			return err
		}
		if err := config.CreateFile(); err != nil {
			return err
		}
		if err := config.WriteKey(key, raw); err != nil {
			return err
		}
		successf(cmd, "Successfully set %q to %v", key, raw)
		return nil
	},
}
