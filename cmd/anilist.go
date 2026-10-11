package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/luevano/mangal/v5/auth/oauth"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/tui/base"
	anilistViewer "github.com/luevano/mangal/v5/tui/model/anilist"
	"github.com/skratchdot/open-golang/open"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var anilistLoginArgs = struct {
	ID      string
	Secret  string
	Browser bool
}{}

func init() {
	rootCmd.AddCommand(anilistCmd)
	anilistCmd.AddCommand(anilistLoginCmd)
	anilistLoginCmd.Flags().StringVar(&anilistLoginArgs.ID, "id", "", "ID of the Anilist API client (asked when missing, the last one is kept)")
	anilistLoginCmd.Flags().StringVar(&anilistLoginArgs.Secret, "secret", "", "Secret of the Anilist API client, without it the login is an implicit grant")
	anilistLoginCmd.Flags().BoolVar(&anilistLoginArgs.Browser, "browser", false, "Also open the address in the browser")
}

var anilistCmd = &cobra.Command{
	Use:   "anilist",
	Short: "Log in to Anilist",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return base.RunStandalone(cmd.Context(), anilistViewer.New())
	},
}

var anilistLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to Anilist without the TUI, also with the browser on another machine",
	Long: `Log in to Anilist with an Anilist API client (https://anilist.co/settings/developer,
redirect URL ` + oauth.AnilistServerCallbackURL + `), for headless use.

The authorize address is printed: open it in any browser and allow mangal. A browser on
this machine comes back to mangal by itself; on another machine the page it ends up on
won't load, paste its address (it has the token after #access_token=, or the code) here.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		in := bufio.NewReader(cmd.InOrStdin())
		id, secret := anilistLoginArgs.ID, anilistLoginArgs.Secret
		if id == "" {
			savedID, savedSecret, err := anilist.Client()
			if err != nil {
				return err
			}
			label := "Anilist API client ID: "
			if savedID != "" {
				label = fmt.Sprintf("Anilist API client ID (enter for %s): ", savedID)
			}
			if id, err = prompt(cmd, in, label); err != nil {
				return err
			}
			if id == "" {
				id = savedID
			}
			if secret == "" {
				label := "Client secret (empty for an implicit grant): "
				if id == savedID && savedSecret != "" {
					label = `Client secret (enter for the saved one, "-" for an implicit grant): `
				}
				if secret, err = promptSecret(cmd, in, label); err != nil {
					return err
				}
				switch {
				case secret == "-":
					secret = ""
				case secret == "" && id == savedID:
					secret = savedSecret
				}
			}
		}
		user, err := anilist.LoginManual(cmd.Context(), id, secret, manualLogin(cmd, in, anilistLoginArgs.Browser))
		if err != nil {
			return fmt.Errorf("anilist login: %w", err)
		}
		cmd.Printf("Logged in to Anilist as %q\n", user.Name())
		return nil
	},
}

// manualLogin prints the authorize address (and opens it with browser) and takes the
// pasted addresses from in, a line each.
func manualLogin(cmd *cobra.Command, in *bufio.Reader, browser bool) oauth.Manual {
	out := cmd.ErrOrStderr()
	pasted := make(chan string)
	go func() {
		defer close(pasted)
		for {
			line, err := in.ReadString('\n')
			if line = strings.TrimSpace(line); line != "" {
				pasted <- line
			}
			if err != nil {
				return
			}
		}
	}()
	return oauth.Manual{
		Pasted: pasted,
		Show: func(authURL string) {
			fmt.Fprintf(out, "Open this address in a browser and allow mangal:\n\n  %s\n\n", authURL)
			if browser {
				if err := open.Start(authURL); err != nil {
					fmt.Fprintf(out, "Couldn't open the browser: %s\n", err)
				}
			}
			fmt.Fprintln(out, "Waiting for the browser. If it runs on another machine, paste the address of the page")
			fmt.Fprintln(out, "it ends up on (it won't load) and press enter:")
		},
		BadPaste: func(err error) {
			fmt.Fprintf(out, "Can't use that (%s), paste the address again:\n", err)
		},
	}
}

func prompt(cmd *cobra.Command, in *bufio.Reader, label string) (string, error) {
	fmt.Fprint(cmd.ErrOrStderr(), label)
	line, err := in.ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return "", fmt.Errorf("read %s: %w", strings.TrimSuffix(strings.TrimSpace(label), ":"), err)
	}
	return strings.TrimSpace(line), nil
}

// promptSecret doesn't echo the secret in a terminal.
func promptSecret(cmd *cobra.Command, in *bufio.Reader, label string) (string, error) {
	f, ok := cmd.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return prompt(cmd, in, label)
	}
	fmt.Fprint(cmd.ErrOrStderr(), label)
	secret, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return "", fmt.Errorf("read the secret: %w", err)
	}
	return strings.TrimSpace(string(secret)), nil
}
