package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/luevano/mangal/v5/template"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(templatesCmd)
}

var templatesCmd = &cobra.Command{
	Annotations: map[string]string{startupAnnotation: startupNoStorage},
	Use:         "templates",
	Short:       "Name templates commands",
	Args:        cobra.NoArgs,
}

func init() {
	templatesCmd.AddCommand(templatesFuncsCmd)
}

var templatesFuncsCmd = &cobra.Command{
	Use:   "funcs",
	Short: "Show available functions",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		w := styledAdaptive(cmd.OutOrStderr())
		for _, f := range template.Funcs {
			fmt.Fprintln(w, style.Bold.Accent.Render(f.Name))
			fmt.Fprintln(w, style.Normal.Secondary.Render(f.Description))
			fmt.Fprintln(w)
		}
	},
}

var templatesExecArgs = struct {
	Value string
}{}

func init() {
	templatesCmd.AddCommand(templatesExecCmd)

	f := templatesExecCmd.Flags()
	f.StringVarP(&templatesExecArgs.Value, "value", "V", `{"Title":"Example Title","Number":32.5}`, "JSON object to use as value")
}

var templatesExecCmd = &cobra.Command{
	Use:   "exec template...",
	Short: "Execute template",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var value map[string]any
		if err := json.Unmarshal([]byte(templatesExecArgs.Value), &value); err != nil {
			return fmt.Errorf("parse --value: %w", err)
		}
		out, err := template.Exec(strings.Join(args, " "), value)
		if err != nil {
			return err
		}
		cmd.Println(out)
		return nil
	},
}
