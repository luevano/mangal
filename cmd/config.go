package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/luevano/mangal/config"
	"github.com/luevano/mangal/theme/style"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
)

func completionConfigKeys(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	keys := config.Keys()

	filtered := lo.Filter(keys, func(key string, _ int) bool {
		return strings.HasPrefix(key, toComplete)
	})

	return filtered, cobra.ShellCompDirectiveDefault
}

func configCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
		Args:  cobra.NoArgs,
	}

	c.AddCommand(configInfoCmd())
	c.AddCommand(configWriteCmd())
	c.AddCommand(configGetCmd())
	c.AddCommand(configSetCmd())

	return c
}

func configInfoCmd() *cobra.Command {
	configInfoArgs := struct {
		JSON  bool
		Short bool
	}{}

	c := &cobra.Command{
		Use:   "info",
		Short: "Show configuration information",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			type configEntry struct {
				Value       any    `json:"value"`
				Default     any    `json:"default"`
				Description string `json:"description"`
			}

			configEntries := map[string]configEntry{}
			for _, field := range config.Fields {
				configEntries[field.Key] = configEntry{
					Value:       config.Get(field.Key),
					Default:     field.Default,
					Description: field.Description,
				}
			}

			if configInfoArgs.JSON {
				jsonEntries, err := json.Marshal(configEntries)
				if err != nil {
					errorf(cmd, err.Error())
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

	c.Flags().BoolVarP(&configInfoArgs.JSON, "json", "j", false, "JSON output")
	c.Flags().BoolVarP(&configInfoArgs.Short, "short", "s", false, "Only print 'key: value'")
	c.MarkFlagsMutuallyExclusive("json", "short")

	return c
}

func configWriteCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "write",
		Short: "Write configuration to disk",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			if err := config.Write(); err != nil {
				errorf(cmd, err.Error())
			}

			successf(cmd, "Wrote config to the file")
		},
	}

	return c
}

func configGetCmd() *cobra.Command {
	c := &cobra.Command{
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

	return c
}

func configSetCmd() *cobra.Command {
	c := &cobra.Command{
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
					errorf(cmd, err.Error())
				}

				converted = int(parsedInt)
			case bool:
				parsedBool, err := strconv.ParseBool(value)
				if err != nil {
					errorf(cmd, err.Error())
				}

				converted = parsedBool
			default:
				errorf(cmd, "unknown value type")
			}

			if err := config.Set(key, converted); err != nil {
				errorf(cmd, err.Error())
			}

			if err := config.Write(); err != nil {
				errorf(cmd, err.Error())
			}

			successf(cmd, "Successfully set %q to %v", key, converted)
		},
	}

	return c
}
