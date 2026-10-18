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
each with its permissions: read (browse, read, the events), progress (the reader records
reads: history, tracker progress), watch (add, change, check the watches), write (the
files, the library, the config, downloads). A running server sees changes within seconds;
a new password or new permissions end the user's logins. No server needs to run: they're
changed in the data db of this config.`,
	Args: cobra.NoArgs,
}

var userArgs = struct {
	Perms []string
}{}

func init() {
	userCmd.AddCommand(userAddCmd)
	userAddCmd.Flags().StringSliceVar(&userArgs.Perms, "perms", nil,
		"Permissions: read, progress, watch, write (comma separated; every one for the first user, read and progress for the others)")
}

var userAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a user",
	Long: `Add a user; the password is asked twice (without echo), or read from stdin (one line).
The first user gets every permission, the others read and progress unless --perms says.`,
	Example: `  mangal user add david
  mangal user add guest --perms read
  mangal user add kid --perms read,progress`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := core.CheckUserName(args[0]); err != nil {
			return err
		}
		password, err := askPassword(cmd)
		if err != nil {
			return err
		}
		u, err := core.NewUsers().Add(ctxOf(cmd), args[0], password, userArgs.Perms)
		if err != nil {
			return err
		}
		successf(cmd, "added the user %q (%s)", u.Name, strings.Join(u.Perms, ", "))
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

func init() {
	userCmd.AddCommand(userPermsCmd)
}

var userPermsCmd = &cobra.Command{
	Use:     "perms <name> <permissions>",
	Short:   "Set a user's permissions (their logins end)",
	Example: `  mangal user perms guest read,progress`,
	Args:    cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := core.NewUsers().SetPerms(ctxOf(cmd), args[0], strings.Split(args[1], ",")); err != nil {
			return err
		}
		successf(cmd, "changed the permissions of %q, its logins ended", args[0])
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
			if !u.LastLogin.IsZero() {
				last = "last login " + u.LastLogin.Format(time.DateTime)
			}
			cmd.Printf("%s  %s  (%s)\n", u.Name, strings.Join(u.Perms, ", "), last)
		}
		return nil
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
