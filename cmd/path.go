package cmd

import (
	"encoding/json"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/path"
	pathViewer "github.com/luevano/mangal/v5/tui/model/path"
	"github.com/luevano/mangal/v5/util"
	"github.com/spf13/cobra"
)

var pathArgs = struct {
	Config    bool
	Cache     bool
	Temp      bool
	Downloads bool
	Providers bool
	Logs      bool
	JSON      bool
}{}

func init() {
	rootCmd.AddCommand(pathCmd)

	f := pathCmd.Flags()
	f.BoolVar(&pathArgs.Config, "cfg", false, "Get config directory")
	f.BoolVar(&pathArgs.Cache, "cache", false, "Get cache directory")
	f.BoolVar(&pathArgs.Temp, "temp", false, "Get temporary directory")
	f.BoolVar(&pathArgs.Downloads, "downloads", false, "Get downloads directory")
	f.BoolVar(&pathArgs.Providers, "providers", false, "Get providers directory")
	f.BoolVar(&pathArgs.Logs, "logs", false, "Get logs directory")
	f.BoolVarP(&pathArgs.JSON, "json", "j", false, "Output as JSON")

	pathCmd.MarkFlagsMutuallyExclusive(
		"cfg",
		"cache",
		"temp",
		"downloads",
		"providers",
		"logs",
	)
}

// TODO: refactor
var pathCmd = &cobra.Command{
	Use:   "path",
	Short: "Show paths",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		var p path.PathName
		switch {
		case pathArgs.Cache:
			p = path.PathCache
		case pathArgs.Config:
			p = path.PathConfig
		case pathArgs.Downloads:
			p = path.PathConfig
		case pathArgs.Temp:
			p = path.PathConfig
		case pathArgs.Providers:
			p = path.PathConfig
		case pathArgs.Logs:
			p = path.PathConfig
		}
		paths := path.AllPaths()
		if p != "" {
			paths = paths.GetAsPaths(p)
		}

		if pathArgs.JSON {
			err := json.NewEncoder(cmd.OutOrStdout()).Encode(paths)
			if err != nil {
				errorf(cmd, "%s", util.ErrorTree(err))
			}
			return
		}

		if p != "" {
			cmd.Println(paths.Get(p))
			return
		}

		if _, err := tea.NewProgram(pathViewer.New(true)).Run(); err != nil {
			errorf(cmd, "%s", util.ErrorTree(err))
		}
	},
}
