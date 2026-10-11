package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/util"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(configCmd)
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
	Args:  cobra.NoArgs,
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
	Run: func(cmd *cobra.Command, _ []string) {
		type configEntry struct {
			Value       any    `json:"value"`
			Default     any    `json:"default"`
			Description string `json:"description"`
		}

		configEntries := map[string]configEntry{}
		for _, key := range config.Keys() {
			configEntries[key] = configEntry{
				Value:       config.Get(key),
				Default:     config.Default(key),
				Description: config.Description(key),
			}
		}

		if configInfoArgs.JSON {
			jsonEntries, err := json.Marshal(configEntries)
			if err != nil {
				errorf(cmd, "%s", util.ErrorTree(err))
			}
			cmd.Println(string(jsonEntries))
			return
		}

		// TODO: change theme?
		for key, entry := range configEntries {
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
	},
}

func init() {
	configCmd.AddCommand(configWriteCmd)
}

var configWriteCmd = &cobra.Command{
	Use:   "write",
	Short: "Write configuration to disk",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		if err := config.Write(); err != nil {
			errorf(cmd, "%s", util.ErrorTree(err))
		}

		successf(cmd, "Wrote config to the file")
	},
}

func init() {
	configCmd.AddCommand(configGetCmd)
}

var configGetCmd = &cobra.Command{
	Use:               "get key",
	Short:             "Get config value by key",
	Args:              cobra.ExactArgs(1),
	SilenceErrors:     true,
	ValidArgsFunction: completionConfigKeys,
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]
		if !config.Exists(key) {
			errorf(cmd, "config key %q doesn't exist", key)
		}

		cmd.Println(config.Get(key))
	},
}

func init() {
	configCmd.AddCommand(configSetCmd)
}

var configSetCmd = &cobra.Command{
	Use:               "set key value",
	Short:             "Sets value to the config key",
	Args:              cobra.ExactArgs(2),
	SilenceErrors:     true,
	ValidArgsFunction: completionConfigKeys,
	Run: func(cmd *cobra.Command, args []string) {
		key, value := args[0], args[1]

		var converted any

		// All of this seems innecessary.
		switch config.Get(key).(type) {
		case nil:
			errorf(cmd, "unknown config key %q", key)
		case string:
			converted = value
		case int:
			parsedInt, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				errorf(cmd, "%s", util.ErrorTree(err))
			}

			converted = int(parsedInt)
		case bool:
			parsedBool, err := strconv.ParseBool(value)
			if err != nil {
				errorf(cmd, "%s", util.ErrorTree(err))
			}

			converted = parsedBool
		default:
			errorf(cmd, "unknown value type")
		}

		if err := config.Set(key, converted); err != nil {
			errorf(cmd, "%s", util.ErrorTree(err))
		}

		if err := config.Write(); err != nil {
			errorf(cmd, "%s", util.ErrorTree(err))
		}

		successf(cmd, "Successfully set %q to %v", key, converted)
	},
}
