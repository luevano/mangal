package cmd

import (
	"cmp"
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

var scriptAPIArgs = struct {
	Server string
	Write  bool
}{}

func init() {
	rootCmd.AddCommand(scriptCmd)
	f := scriptCmd.Flags()

	f.StringVarP(&scriptArgs.File, "file", "f", "", "Read script from file")
	f.StringVarP(&scriptArgs.String, "string", "s", "", "Read script from the given string")
	f.BoolVarP(&scriptArgs.Stdin, "stdin", "i", false, "Read script from stdin")
	f.StringVarP(&scriptArgs.Provider, "provider", "p", "", "Load provider by tag")
	f.StringToStringVarP(&scriptArgs.Variables, "vars", "V", nil, "Variables to set in the `Vars` table")
	f.StringVar(&scriptAPIArgs.Server, "server", "", "mangal.api's server (its token in MANGAL_API_TOKEN), instead of the connection's or this config's")
	f.BoolVar(&scriptAPIArgs.Write, "write", false, "mangal.api without a mangal serve (the API in this process): download and manage on top of script.api_scopes")
	addLoaderFlags(f)

	scriptCmd.MarkFlagRequired("provider")
	scriptCmd.MarkFlagsOneRequired("file", "string", "stdin")
	scriptCmd.MarkFlagsMutuallyExclusive("file", "string", "stdin")
	scriptCmd.RegisterFlagCompletionFunc("provider", completionProviderIDs)
}

var scriptCmd = &cobra.Command{
	Use:   "script",
	Short: "Useful for custom process with Lua",
	Long: `script, useful for custom process with Lua. On a server (the connection) the provider is the
server's, browsed through it; its mangal.api is the server's API, its download_chapter refuses
(it writes on this machine). Locally mangal.api is the running mangal serve of this config, else
the API in this process with script.api_scopes (--write adds download and manage): its downloads
run in this process, the script waits for them at the end (ctrl+c cancels).`,
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

		args := scriptArgs
		var err error
		if args.Server, err = connectRemote(cmd); err != nil {
			return err
		}
		if args.Server != nil {
			sayOn(cmd, args.Server.URL)
		}
		var done func() error
		args.API, done = scriptAPI(cmd, scriptAPIArgs.Server, scriptAPIArgs.Write)
		err = script.Run(cmd.Context(), args, reader)
		return cmp.Or(err, done())
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
