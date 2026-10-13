package cmd

import (
	"bufio"
	"errors"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(notifyCmd)
	notifyCmd.AddCommand(notifyWebhookCmd)
	notifyWebhookCmd.Flags().BoolVar(&notifyWebhookArgs.Clear, "clear", false, "Remove the saved webhook")
}

var notifyCmd = &cobra.Command{
	Use:   "notify",
	Short: "Set up the notifications",
	Args:  cobra.NoArgs,
}

var notifyWebhookArgs = struct {
	Clear bool
}{}

var notifyWebhookCmd = &cobra.Command{
	Use:   "webhook",
	Short: "Save the Discord webhook",
	Long: `Save the Discord webhook the notifications go to (after "mangal inline download", watch
checks, and TUI downloads with notification.tui). It's kept with the logins (the OS keyring
with auth.keyring); notification.discord.webhook_url is only used when none is saved.
The url is read from the terminal without echo, or from stdin.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if notifyWebhookArgs.Clear {
			if err := cache.SetDiscordWebhook(""); err != nil {
				return err
			}
			successf(cmd, "removed the saved Discord webhook")
			return nil
		}
		url, err := readSecret(cmd, bufio.NewReader(cmd.InOrStdin()), "Discord webhook URL: ")
		if err != nil {
			return err
		}
		if url == "" {
			return errors.New("no webhook given (--clear removes the saved one)")
		}
		if err := config.ValidateWebhookURL(url); err != nil {
			return err
		}
		if err := cache.SetDiscordWebhook(url); err != nil {
			return err
		}
		successf(cmd, "saved the Discord webhook")
		return nil
	},
}
