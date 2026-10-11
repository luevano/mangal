package cmd

import (
	"encoding/json"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/path"
	pathViewer "github.com/luevano/mangal/v5/tui/model/path"
	"github.com/spf13/cobra"
)

var pathArgs = struct {
	Config    bool
	Cache     bool
	Temp      bool
	Downloads bool
	Providers bool
	Logs      bool
	Data      bool
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
	f.BoolVar(&pathArgs.Data, "data", false, "Get data directory")
	f.BoolVarP(&pathArgs.JSON, "json", "j", false, "Output as JSON")

	pathCmd.MarkFlagsMutuallyExclusive(
		"cfg",
		"cache",
		"temp",
		"downloads",
		"providers",
		"logs",
		"data",
	)
}

var pathCmd = &cobra.Command{
	Annotations: map[string]string{startupAnnotation: startupNoStorage},
	Use:         "path",
	Short:       "Show paths",
	Args:        cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		var p path.PathName
		switch {
		case pathArgs.Cache:
			p = path.PathCache
		case pathArgs.Config:
			p = path.PathConfig
		case pathArgs.Downloads:
			p = path.PathDownloads
		case pathArgs.Temp:
			p = path.PathTemp
		case pathArgs.Providers:
			p = path.PathProviders
		case pathArgs.Logs:
			p = path.PathLog
		case pathArgs.Data:
			p = path.PathData
		}
		paths := path.All()
		if p != "" {
			paths = paths.GetAsPaths(p)
		}

		if pathArgs.JSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(paths)
		}

		if p != "" {
			cmd.Println(paths.Get(p))
			return nil
		}

		_, err := tea.NewProgram(pathViewer.New(true)).Run()
		return err
	},
}
