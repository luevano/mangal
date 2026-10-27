package cmd

import (
	"strings"

	"github.com/luevano/mangal/v5/core"
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
users ("mangal user"). Only a token's hash is kept, it's printed once when made. A token
has a role or permissions as users do ("mangal user roles --all"), read by default; one
made here is the server's (no owner). No server needs to run; a running one sees changes
within seconds.`,
	Args: cobra.NoArgs,
}

var tokenAddArgs = struct {
	Role   string
	Scopes []string
}{}

func init() {
	tokenCmd.AddCommand(tokenAddCmd)
	permsFlags(tokenAddCmd, &tokenAddArgs.Role, &tokenAddArgs.Scopes)
	tokenAddCmd.Flags().StringSliceVar(&tokenAddArgs.Scopes, "scope", nil, "Same as --perms")
	_ = tokenAddCmd.Flags().MarkHidden("scope")
}

var tokenAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Make an API token, printed once",
	Example: `  mangal token add claude
  mangal token add backup-script --role manager`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		scopes, err := core.WithRole(tokenAddArgs.Role, tokenAddArgs.Scopes)
		if err != nil {
			return err
		}
		return generateNamedToken(cmd, args[0], scopes)
	},
}

var tokenPermsArgs = struct {
	Role  string
	Perms []string
}{}

func init() {
	tokenCmd.AddCommand(tokenPermsCmd)
	permsFlags(tokenPermsCmd, &tokenPermsArgs.Role, &tokenPermsArgs.Perms)
}

var tokenPermsCmd = &cobra.Command{
	Use:   "perms <name>",
	Short: "Set an API token's role or permissions (its secret stays)",
	Example: `  mangal token perms claude --perms read
  mangal token perms backup-script --role manager`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		perms, err := core.WithRole(tokenPermsArgs.Role, tokenPermsArgs.Perms)
		if err != nil {
			return err
		}
		if err := core.NewUsers().SetTokenScopes(ctxOf(cmd), args[0], perms); err != nil {
			return err
		}
		scopes, _ := core.NormalizeScopes(perms)
		successf(cmd, "the API token %q has %s now", args[0], strings.Join(scopes, ", "))
		return nil
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
