package cmd

import (
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(tokenCmd)
}

var tokenCmd = &cobra.Command{
	Use:     "token",
	Aliases: []string{"tokens"},
	Short:   "Manage the API tokens of mangal serve",
	Long: `API tokens are for programs calling the API of mangal serve (scripts, "mangal api
--server", other apps): Authorization: Bearer <token>. They aren't logins: people log in as
users ("mangal user"). Only a token's hash is kept, it's printed once when made. Scopes:
read (GET, events), watch (the watches: add, change, check), progress (the reader's reads:
history, tracker progress), write (the files, the library, the config, downloads). No server
needs to run; a running one sees changes within seconds.`,
	Args: cobra.NoArgs,
}

var tokenAddArgs = struct {
	Scopes []string
}{}

func init() {
	tokenCmd.AddCommand(tokenAddCmd)
	tokenAddCmd.Flags().StringSliceVar(&tokenAddArgs.Scopes, "scope", nil, "Its scopes: read, watch, progress, write (comma separated or repeated; read by default)")
}

var tokenAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Make an API token, printed once",
	Example: `  mangal token add claude
  mangal token add backup-script --scope read,write`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return generateNamedToken(cmd, args[0], tokenAddArgs.Scopes)
	},
}

func init() {
	tokenCmd.AddCommand(tokenLsCmd)
}

var tokenLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List the API tokens, with their scopes",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return listServeTokens(cmd)
	},
}

func init() {
	tokenCmd.AddCommand(tokenRmCmd)
}

var tokenRmCmd = &cobra.Command{
	Use:     "rm <name>",
	Aliases: []string{"remove", "revoke"},
	Short:   "Remove an API token (a running server stops taking it within seconds)",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return revokeServeToken(cmd, args[0])
	},
}
