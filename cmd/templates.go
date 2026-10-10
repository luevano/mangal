package cmd

import (
	"encoding/json"
	"github.com/luevano/mangal/util"
	"strings"
	"text/template"

	"github.com/luevano/mangal/template/funcs"
	"github.com/luevano/mangal/theme/style"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(templatesCmd)
}

var templatesCmd = &cobra.Command{
	Use:   "templates",
	Short: "Name templates commands",
	Args:  cobra.NoArgs,
}

func init() {
	templatesCmd.AddCommand(templatesFuncsCmd)
}

var templatesFuncsCmd = &cobra.Command{
	Use:   "funcs",
	Short: "Show available functions",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		for k, v := range funcs.Funcs {
			cmd.Println(style.Bold.Accent.Render(k))
			cmd.Println(style.Normal.Secondary.Render(v.Description))
			cmd.Println()
		}
	},
}

var templatesExecArgs = struct {
	Value string
}{}

func init() {
	templatesCmd.AddCommand(templatesExecCmd)

	exampleValue := struct {
		Title  string
		Number float64
	}{
		Title:  "Example Title",
		Number: 32.5,
	}
	marshalled := lo.Must(json.Marshal(exampleValue))

	f := templatesExecCmd.Flags()
	f.StringVarP(&templatesExecArgs.Value, "value", "v", string(marshalled), "JSON object to use as value")
}

var templatesExecCmd = &cobra.Command{
	Use:   "exec template...",
	Short: "Execute template",
	Args:  cobra.MinimumNArgs(1),
	// TODO: fix issue when using spaces in -v
	Run: func(cmd *cobra.Command, args []string) {
		tmpl, err := template.
			New("exec").
			Funcs(funcs.FuncMap).
			Parse(strings.Join(args, " "))
		if err != nil {
			errorf(cmd, "%s", util.ErrorTree(err))
		}

		var value map[string]any

		if err := json.Unmarshal([]byte(templatesExecArgs.Value), &value); err != nil {
			errorf(cmd, "%s", util.ErrorTree(err))
		}

		if err := tmpl.Execute(cmd.OutOrStdout(), value); err != nil {
			errorf(cmd, "%s", util.ErrorTree(err))
		}

		cmd.Println()
	},
}
