package cmd

import (
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/provider/info"
	"github.com/luevano/mangal/v5/provider/install"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(providersCmd)
}

var providersCmd = &cobra.Command{
	Use:   "providers",
	Short: "Providers management",
	Args:  cobra.NoArgs,
}

func init() {
	providersCmd.AddCommand(providersAddCmd)
}

var providersAddCmd = &cobra.Command{
	Use:   "add <url>",
	Short: "Install provider",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := path.Ensure(path.Providers())
		if err != nil {
			return err
		}
		providerInfo, err := install.Add(cmd.Context(), dir, args[0], cmd.ErrOrStderr())
		if err != nil {
			return err
		}
		successf(cmd, "Installed %s (%s)", providerInfo.Name, providerInfo.ID)
		return nil
	},
}

func init() {
	providersCmd.AddCommand(providersUpCmd)
}

var providersUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Update providers",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := path.Ensure(path.Providers())
		if err != nil {
			return err
		}
		return install.Update(cmd.Context(), dir, cmd.ErrOrStderr())
	},
}

func init() {
	providersCmd.AddCommand(providersLsCmd)
	providersLsCmd.Flags().BoolP("mirrors", "m", false, "Also list each provider's website and mirrors (for providers.mirrors)")
}

var providersLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List installed providers",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		loaders, problems, err := manager.Load()
		if err != nil {
			return err
		}
		for _, p := range problems {
			cmd.PrintErrf("warning: skipped %s\n", p)
		}

		withMirrors, _ := cmd.Flags().GetBool("mirrors")
		for _, loader := range loaders {
			info := loader.Info()
			cmd.Println(info.ID)
			if withMirrors && len(info.Mirrors) != 0 {
				preferred := config.Providers.Mirrors.Get()[info.ID]
				for _, site := range append([]string{info.Website}, info.Mirrors...) {
					mark := ""
					if site == preferred || (preferred == "" && site == info.Website) {
						mark = " (preferred)"
					}
					cmd.Printf("  %s%s\n", site, mark)
				}
			}
		}

		return nil
	},
}

func init() {
	providersCmd.AddCommand(providersRmCmd)
}

var providersRmCmd = &cobra.Command{
	Use:   "rm tags",
	Short: "Remove provider",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, tag := range args {
			if err := install.Remove(path.Providers(), tag); err != nil {
				return err
			}
		}
		return nil
	},
}

func init() {
	providersCmd.AddCommand(providersNewCmd)

	f := providersNewCmd.Flags()
	f.StringP("directory", "d", "", "Directory in which new providers will be created")
	configKeyFlag(f, "directory", config.Providers.Path.Key)

	providersNewCmd.MarkFlagDirname("directory")
}

// TODO: provide more options for the new provider
var providersNewCmd = &cobra.Command{
	Use:   "new",
	Short: "Create new provider",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		dir, err := path.Ensure(path.Providers())
		if err != nil {
			return err
		}
		return install.New(dir, info.Info{
			ProviderInfo: libmangal.ProviderInfo{
				ID:          "test",
				Name:        "test",
				Version:     "0.1.0",
				Description: "Lorem ipsum",
				Website:     "example.com",
			},
			Type: info.TypeLua,
		})
	},
}
