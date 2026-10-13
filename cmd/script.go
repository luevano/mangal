package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/script"
	"github.com/luevano/mangal/v5/script/lib"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/spf13/cobra"
)

var scriptArgs = script.Args{}

func init() {
	rootCmd.AddCommand(scriptCmd)
	f := scriptCmd.Flags()

	f.StringVarP(&scriptArgs.File, "file", "f", "", "Read script from file")
	f.StringVarP(&scriptArgs.String, "string", "s", "", "Read script from the given string")
	f.BoolVarP(&scriptArgs.Stdin, "stdin", "i", false, "Read script from stdin")
	f.StringVarP(&scriptArgs.Provider, "provider", "p", "", "Load provider by tag")
	f.StringToStringVarP(&scriptArgs.Variables, "vars", "V", nil, "Variables to set in the `Vars` table")
	addLoaderFlags(f)

	scriptCmd.MarkFlagRequired("provider")
	scriptCmd.MarkFlagsOneRequired("file", "string", "stdin")
	scriptCmd.MarkFlagsMutuallyExclusive("file", "string", "stdin")
	scriptCmd.RegisterFlagCompletionFunc("provider", completionProviderIDs)
}

var scriptCmd = &cobra.Command{
	Use:     "script",
	Short:   "Useful for custom process with Lua",
	Long:    "script, useful for custom process with Lua",
	GroupID: groupMode,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		var reader io.Reader

		switch {
		case cmd.Flag("file").Changed:
			file, err := afs.Afero.Open(scriptArgs.File)
			if err != nil {
				return fmt.Errorf("open script: %w", err)
			}
			defer file.Close()
			reader = file
		case cmd.Flag("string").Changed:
			reader = strings.NewReader(scriptArgs.String)
		case cmd.Flag("stdin").Changed:
			reader = cmd.InOrStdin()
		}

		return script.Run(cmd.Context(), scriptArgs, reader)
	},
}

var scriptDocArgs = struct {
	Force bool
}{}

func init() {
	scriptCmd.AddCommand(scriptDocCmd)
	scriptDocCmd.Flags().BoolVar(&scriptDocArgs.Force, "force", false, "Overwrite an existing "+meta.AppName+".lua")
}

var scriptDocCmd = &cobra.Command{
	Use:         "doc",
	Short:       "Generate documentation for the `mangal` lua library",
	Long:        "Write the specs of the `mangal` lua library to ./" + meta.AppName + ".lua, an existing file only with --force.",
	Args:        cobra.NoArgs,
	Annotations: map[string]string{startupAnnotation: startupNoStorage},
	RunE: func(cmd *cobra.Command, _ []string) error {
		filename := meta.AppName + ".lua"
		flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		if !scriptDocArgs.Force {
			flags |= os.O_EXCL
		}
		file, err := afs.Afero.OpenFile(filename, flags, config.Download.ModeFile.Get())
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s exists, --force overwrites it", filename)
		}
		if err != nil {
			return fmt.Errorf("write library specs: %w", err)
		}
		_, err = io.WriteString(file, lib.LuaDoc())
		if cerr := file.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fmt.Errorf("write library specs: %w", err)
		}
		successf(cmd, "library specs written to %s", filename)
		return nil
	},
}
