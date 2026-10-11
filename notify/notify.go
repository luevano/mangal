package notify

import (
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/notify/discord"
	"github.com/luevano/mangal/v5/util/chapter"
)

// Send will send a notification to the configured services.
func Send(chapters chapter.Chapters) error {
	if config.Notification.Discord.WebhookURL.Get() != "" {
		return discord.Send(chapters)
	}
	return nil
}

// SendError is a wrapper error handling that will send a notification
// to configured services and return the same error back.
func SendError(err error) error {
	if config.Notification.Discord.WebhookURL.Get() != "" {
		return discord.SendError(err)
	}
	return err
}
