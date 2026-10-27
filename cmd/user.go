package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/luevano/mangal/v5/core"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(userCmd)
}

var userCmd = &cobra.Command{
	Use:     "user",
	Aliases: []string{"users"},
	Short:   "Manage the users who log into mangal serve",
	Long: `The users log into mangal serve: the web UI's login page, or basic auth (name and
password) for OPDS readers. They're kept in the data db (passwords as argon2id hashes),
each with a role (reader, downloader, manager, admin) or their own set of permissions:
"mangal user roles --all" says what each allows. A running server sees changes within
seconds; a new password or new permissions end the user's logins. No server needs to run:
they're changed in the data db of this config. There's always one: "mangal", an admin
without a password, is made when there's none (mangal on this machine is a user too, see
auth.user); a user without a password can't log in, "mangal user passwd" gives one.`,
	Args: cobra.NoArgs,
}

var userArgs = struct {
	Role  string
	Perms []string
}{}

// permsFlags are --role and --perms (added to the role's).
func permsFlags(cmd *cobra.Command, role *string, perms *[]string) {
	cmd.Flags().StringVar(role, "role", "", "A role: reader, downloader, manager, admin (mangal user roles)")
	cmd.Flags().StringSliceVar(perms, "perms", nil, "Permissions on top of the role, or instead of it: "+strings.Join(core.Scopes, ", "))
}

func init() {
	userCmd.AddCommand(userAddCmd)
	permsFlags(userAddCmd, &userArgs.Role, &userArgs.Perms)
}

var userAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a user",
	Long: `Add a user; the password is asked twice (without echo), or read from stdin (one line).
The first user is an admin, the others readers unless --role or --perms say.`,
	Example: `  mangal user add david
  mangal user add kid
  mangal user add friend --role downloader --perms tokens
  mangal user add guest --perms read`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := core.CheckUserName(args[0]); err != nil {
			return err
		}
		password, err := askPassword(cmd)
		if err != nil {
			return err
		}
		perms, err := core.WithRole(userArgs.Role, userArgs.Perms)
		if err != nil {
			return err
		}
		u, err := core.NewUsers().Add(ctxOf(cmd), args[0], password, perms)
		if err != nil {
			return err
		}
		successf(cmd, "added the user %q (%s: %s)", u.Name, u.Role, strings.Join(u.Perms, ", "))
		return nil
	},
}

func init() {
	userCmd.AddCommand(userPasswdCmd)
}

var userPasswdCmd = &cobra.Command{
	Use:   "passwd <name>",
	Short: "Change a user's password (their logins end)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		password, err := askPassword(cmd)
		if err != nil {
			return err
		}
		if err := core.NewUsers().SetPassword(ctxOf(cmd), args[0], password); err != nil {
			return err
		}
		successf(cmd, "changed the password of %q, its logins ended", args[0])
		return nil
	},
}

var userPermsArgs = struct {
	Role  string
	Perms []string
}{}

func init() {
	userCmd.AddCommand(userPermsCmd)
	permsFlags(userPermsCmd, &userPermsArgs.Role, &userPermsArgs.Perms)
}

var userPermsCmd = &cobra.Command{
	Use:   "perms <name> [permissions]",
	Short: "Set a user's role or permissions (their logins end)",
	Example: `  mangal user perms kid --role downloader
  mangal user perms friend --role reader --perms tokens
  mangal user perms guest read,progress`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		perms := userPermsArgs.Perms
		if len(args) == 2 {
			perms = append(perms, strings.Split(args[1], ",")...)
		}
		perms, err := core.WithRole(userPermsArgs.Role, perms)
		if err != nil {
			return err
		}
		if err := core.NewUsers().SetPerms(ctxOf(cmd), args[0], perms); err != nil {
			return err
		}
		successf(cmd, "changed the permissions of %q, its logins ended", args[0])
		return nil
	},
}

func init() {
	userCmd.AddCommand(userRenameCmd)
}

var userRenameCmd = &cobra.Command{
	Use:   "rename <name> <new name>",
	Short: "Rename a user, with their history, tokens and auth.user (their logins end)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := core.NewUsers().Rename(ctxOf(cmd), args[0], args[1]); err != nil {
			return err
		}
		successf(cmd, "renamed the user %q to %q", args[0], args[1])
		return nil
	},
}

func init() {
	userCmd.AddCommand(userLogoutCmd)
}

var userLogoutCmd = &cobra.Command{
	Use:   "logout <name>",
	Short: "End a user's logins (every browser and app)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := core.NewUsers().EndLogins(ctxOf(cmd), args[0]); err != nil {
			return err
		}
		successf(cmd, "ended the logins of %q", args[0])
		return nil
	},
}

func init() {
	userCmd.AddCommand(userRmCmd)
}

var userRmCmd = &cobra.Command{
	Use:     "rm <name>",
	Aliases: []string{"remove"},
	Short:   "Remove a user (their logins end)",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := core.NewUsers().Remove(ctxOf(cmd), args[0]); err != nil {
			return err
		}
		successf(cmd, "removed the user %q", args[0])
		return nil
	},
}

func init() {
	userCmd.AddCommand(userLsCmd)
}

var userLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List the users",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		list, err := core.NewUsers().List(ctxOf(cmd))
		if err != nil {
			return err
		}
		if len(list) == 0 {
			cmd.Println("no users (mangal user add <name>)")
			return nil
		}
		for _, u := range list {
			last := "never logged in"
			switch {
			case !u.Password:
				last = "no password: only mangal on this machine"
			case !u.LastLogin.IsZero():
				last = "last login " + u.LastLogin.Format(time.DateTime)
			}
			cmd.Printf("%s  %s: %s  (%s)\n", u.Name, u.Role, strings.Join(u.Perms, ", "), last)
		}
		return nil
	},
}

var userRolesArgs = struct {
	All bool
}{}

func init() {
	userCmd.AddCommand(userRolesCmd)
	userRolesCmd.Flags().BoolVar(&userRolesArgs.All, "all", false, "Also every permission, with what it allows")
}

var userRolesCmd = &cobra.Command{
	Use:   "roles",
	Short: "What each role (and with --all each permission) allows",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		for _, r := range core.Roles {
			cmd.Printf("%-10s  %s\n            (%s)\n", r.Name, r.Line, strings.Join(r.Perms, ", "))
		}
		cmd.Println("tokens can be added to any of them (--perms tokens): they make their own API tokens.")
		if !userRolesArgs.All {
			return
		}
		cmd.Println()
		for _, p := range core.Permissions {
			implies := ""
			if len(p.Implies) != 0 {
				implies = " [with " + strings.Join(p.Implies, ", ") + "]"
			}
			cmd.Printf("%-10s  %s%s\n", p.Name, p.Line, implies)
		}
	},
}

// askPassword reads a new password (twice on a terminal).
func askPassword(cmd *cobra.Command) (string, error) {
	in := bufio.NewReader(cmd.InOrStdin())
	password, err := readSecret(cmd, in, "Password: ")
	if err != nil {
		return "", err
	}
	if len(password) < core.MinPasswordLen {
		return "", fmt.Errorf("the password needs %d characters or more", core.MinPasswordLen)
	}
	if _, ok := terminal(cmd.InOrStdin()); ok {
		again, err := readSecret(cmd, in, "Again: ")
		if err != nil {
			return "", err
		}
		if again != password {
			return "", errors.New("the passwords differ")
		}
	}
	return password, nil
}
