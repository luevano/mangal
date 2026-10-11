// Package notify sends download notifications (only Discord for now), after `mangal inline
// download`, watch checks and, with notification.tui, TUI downloads.
package notify

import (
	"context"

	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/notify/discord"
	"github.com/luevano/mangal/v5/util/cache"
)

// WebhookURL is the Discord webhook: the saved one (mangal notify webhook), else
// notification.discord.webhook_url. Empty if none.
func WebhookURL() string {
	url, err := cache.DiscordWebhook()
	if err != nil {
		log.Error("discord webhook: %s", err)
	}
	if url != "" {
		return url
	}
	return config.Notification.Discord.WebhookURL.Get()
}

// SendContext sends a summary of the download to the configured services.
func SendContext(ctx context.Context, results client.Results) error {
	if url := WebhookURL(); url != "" {
		return discord.Send(ctx, url, results)
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
	if url := WebhookURL(); url != "" {
		return discord.SendError(ctx, url, err)
	}
	return err
}

// SendError calls SendErrorContext with context.Background().
func SendError(err error) error {
	return SendErrorContext(context.Background(), err)
}

// Watches notifies the watch checks: one message per manga with new chapters, one per
// failed check (watch.notify picks which).
func Watches() watch.Notifier {
	return watchNotifier{}
}

type watchNotifier struct{}

func (watchNotifier) New(ctx context.Context, r watch.Result) error {
	if url := WebhookURL(); url != "" {
		return discord.SendWatch(ctx, url, r, config.Notification.IncludeDirectory.Get())
	}
	return nil
}

func (watchNotifier) Error(ctx context.Context, r watch.Result) error {
	if url := WebhookURL(); url != "" {
		return discord.SendWatchError(ctx, url, r)
	}
	return nil
}
