package cmd

import (
	"fmt"
	"path/filepath"

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
		successf(cmd, "installed %s (%s)", providerInfo.Name, providerInfo.ID)
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

var providersRmArgs = struct {
	Force, Yes bool
}{}

func init() {
	providersCmd.AddCommand(providersRmCmd)
	f := providersRmCmd.Flags()
	f.BoolVar(&providersRmArgs.Force, "force", false, "Remove it even with local work (not a git repository, no remote, uncommitted or unpushed changes)")
	f.BoolVarP(&providersRmArgs.Yes, "yes", "y", false, "Don't ask")
}

var providersRmCmd = &cobra.Command{
	Use:   "rm <dir>...",
	Short: "Remove installed providers by their directory name",
	Long: `Remove installed providers by their directory name, for good. One with local work (not
a git repository, no remote, uncommitted changes or commits on no remote branch) is refused
unless --force. It asks first (--yes doesn't; without a terminal it's needed).`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := path.Providers()
		for _, tag := range args {
			work, err := install.LocalWork(dir, tag)
			if err != nil {
				return err
			}
			question := fmt.Sprintf("Remove provider %q?", tag)
			if work != "" {
				if !providersRmArgs.Force {
					return fmt.Errorf("provider %q has local work (%s): --force removes it anyway", tag, work)
				}
				question = fmt.Sprintf("Remove provider %q and its local work (%s)?", tag, work)
			}
			if !providersRmArgs.Yes && !confirm(cmd, question) {
				cmd.Printf("%s not removed (--yes removes without asking)\n", tag)
				continue
			}
			if err := install.Remove(dir, tag); err != nil {
				return err
			}
			successf(cmd, "removed %s", tag)
		}
		return nil
	},
}

func init() {
	providersCmd.AddCommand(providersNewCmd)

	f := providersNewCmd.Flags()
	f.StringP("directory", "d", "", "Directory in which new providers will be created")
	configKeyFlag(f, "directory", config.Providers.Path.Key)
	f.StringVar(&providersNewArgs.ID, "id", "", "Provider id, also the name of its directory")
	f.StringVar(&providersNewArgs.Name, "name", "", "Provider name (default: the id)")
	f.StringVar(&providersNewArgs.Website, "website", "", "Website the provider scrapes, e.g. https://example.com")
	f.StringVar(&providersNewArgs.Description, "description", "", "Provider description")

	providersNewCmd.MarkFlagDirname("directory")
	providersNewCmd.MarkFlagRequired("id")
}

var providersNewArgs libmangal.ProviderInfo

var providersNewCmd = &cobra.Command{
	Use:   "new",
	Short: "Create a new Lua provider from the template",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		dir, err := path.Ensure(path.Providers())
		if err != nil {
			return err
		}
		providerInfo := providersNewArgs
		if providerInfo.Name == "" {
			providerInfo.Name = providerInfo.ID
		}
		providerInfo.Version = "0.1.0"
		if err := install.New(dir, info.Info{ProviderInfo: providerInfo, Type: info.TypeLua}); err != nil {
			return err
		}
		successf(cmd, "created %s", filepath.Join(dir, providerInfo.ID))
		return nil
	},
}
