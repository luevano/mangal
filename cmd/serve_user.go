package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/luevano/mangal/v5/serve"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/spf13/cobra"
)

func init() {
	serveCmd.AddCommand(serveUserCmd)
}

var serveUserCmd = &cobra.Command{
	Use:   "user",
	Short: "Manage the users who log in",
	Long: `The users log into mangal serve: the web UI's login page, or basic auth (name and
password) for OPDS readers. They're kept in the data db (passwords as argon2id hashes),
each with its permissions: read (browse, read, the events), progress (the reader records
reads: history, tracker progress), watch (add, change, check the watches), write (the
files, the library, the config, downloads). A running server sees changes within seconds;
a new password or new permissions end the user's logins.`,
	Args: cobra.NoArgs,
}

var serveUserArgs = struct {
	Perms []string
}{}

func init() {
	serveUserCmd.AddCommand(serveUserAddCmd)
	serveUserAddCmd.Flags().StringSliceVar(&serveUserArgs.Perms, "perms", nil,
		"Permissions: read, progress, watch, write (comma separated; every one for the first user, read and progress for the others)")
}

var serveUserAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a user",
	Long: `Add a user; the password is asked twice (without echo), or read from stdin (one line).
The first user gets every permission, the others read and progress unless --perms says.`,
	Example: `  mangal serve user add david
  mangal serve user add guest --perms read
  mangal serve user add kid --perms read,progress`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if err := checkUserName(name); err != nil {
			return err
		}
		users, err := serveUsers()
		if err != nil {
			return err
		}
		if _, err := users.Get(name); err == nil {
			return fmt.Errorf("there's a user %q already (passwd changes the password)", name)
		} else if !errors.Is(err, storage.ErrUserNotFound) {
			return err
		}
		perms := serveUserArgs.Perms
		if len(perms) == 0 {
			list, err := users.List()
			if err != nil {
				return err
			}
			perms = []string{serve.ScopeRead, serve.ScopeProgress}
			if len(list) == 0 {
				perms = serve.Scopes
			}
		}
		if perms, err = serve.NormalizeScopes(perms); err != nil {
			return err
		}
		hash, err := askPassword(cmd)
		if err != nil {
			return err
		}
		if err := users.Add(storage.User{Name: name, Hash: hash, Perms: perms}); err != nil {
			return err
		}
		successf(cmd, "added the user %q (%s)", name, strings.Join(perms, ", "))
		return nil
	},
}

func init() {
	serveUserCmd.AddCommand(serveUserPasswdCmd)
}

var serveUserPasswdCmd = &cobra.Command{
	Use:   "passwd <name>",
	Short: "Change a user's password (their logins end)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		users, err := serveUsers()
		if err != nil {
			return err
		}
		if _, err := users.Get(args[0]); err != nil {
			return err
		}
		hash, err := askPassword(cmd)
		if err != nil {
			return err
		}
		if err := users.SetHash(args[0], hash); err != nil {
			return err
		}
		successf(cmd, "changed the password of %q, its logins ended", args[0])
		return nil
	},
}

func init() {
	serveUserCmd.AddCommand(serveUserPermsCmd)
}

var serveUserPermsCmd = &cobra.Command{
	Use:     "perms <name> <permissions>",
	Short:   "Set a user's permissions (their logins end)",
	Example: `  mangal serve user perms guest read,progress`,
	Args:    cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		perms, err := serve.NormalizeScopes(strings.Split(args[1], ","))
		if err != nil {
			return err
		}
		if len(perms) == 0 {
			return errors.New("no permissions given (read, progress, watch, write)")
		}
		users, err := serveUsers()
		if err != nil {
			return err
		}
		if err := users.SetPerms(args[0], perms); err != nil {
			return err
		}
		successf(cmd, "%q can %s now, its logins ended", args[0], strings.Join(perms, ", "))
		return nil
	},
}

func init() {
	serveUserCmd.AddCommand(serveUserLogoutCmd)
}

var serveUserLogoutCmd = &cobra.Command{
	Use:   "logout <name>",
	Short: "End a user's logins (every browser and app)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		users, err := serveUsers()
		if err != nil {
			return err
		}
		if err := users.EndSessions(args[0]); err != nil {
			return err
		}
		successf(cmd, "ended the logins of %q", args[0])
		return nil
	},
}

func init() {
	serveUserCmd.AddCommand(serveUserRmCmd)
}

var serveUserRmCmd = &cobra.Command{
	Use:     "rm <name>",
	Aliases: []string{"remove"},
	Short:   "Remove a user (their logins end)",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		users, err := serveUsers()
		if err != nil {
			return err
		}
		if err := users.Remove(args[0]); err != nil {
			return err
		}
		successf(cmd, "removed the user %q", args[0])
		return nil
	},
}

func init() {
	serveUserCmd.AddCommand(serveUserLsCmd)
}

var serveUserLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List the users",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		users, err := serveUsers()
		if err != nil {
			return err
		}
		list, err := users.List()
		if err != nil {
			return err
		}
		if len(list) == 0 {
			cmd.Println("no users (mangal serve user add <name>)")
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

func serveUsers() (storage.Users, error) {
	st, err := cache.Storage()
	if err != nil {
		return storage.Users{}, err
	}
	return st.Users(), nil
}

func checkUserName(name string) error {
	if name == "" || strings.TrimSpace(name) != name || strings.ContainsAny(name, ":\x00") {
		return fmt.Errorf("%q can't be a user's name (no spaces around it, no ':')", name)
	}
	return nil
}

// askPassword reads a new password (twice on a terminal) and hashes it.
func askPassword(cmd *cobra.Command) (string, error) {
	in := bufio.NewReader(cmd.InOrStdin())
	password, err := readSecret(cmd, in, "Password: ")
	if err != nil {
		return "", err
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
	return serve.HashPassword(password)
}
