// Package notify sends download notifications (only Discord for now). Only `mangal inline
// download` sends them.
package notify

import (
	"context"

	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/notify/discord"
)

// SendContext sends a summary of the download to the configured services.
func SendContext(ctx context.Context, results client.Results) error {
	if config.Notification.Discord.WebhookURL.Get() != "" {
		return discord.Send(ctx, results)
	}
	return nil
}

// Send calls SendContext with context.Background().
func Send(results client.Results) error {
	return SendContext(context.Background(), results)
}

// SendErrorContext sends the error to the configured services and returns it, joined
// with the error sending it if any.
func SendErrorContext(ctx context.Context, err error) error {
	if config.Notification.Discord.WebhookURL.Get() != "" {
		return discord.SendError(ctx, err)
	}
	return err
}

// SendError calls SendErrorContext with context.Background().
func SendError(err error) error {
	return SendErrorContext(context.Background(), err)
}
