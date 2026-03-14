package discord

import (
	"errors"
	"strconv"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/disgo/webhook"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/config"
	"github.com/luevano/mangal/log"
	"github.com/luevano/mangal/util/chapter"
	stringutil "github.com/luevano/mangal/util/string"
)

const (
	ColorRed    = 15548997
	ColorOrange = 15105570
	ColorGreen  = 5763719
)

type DiscordNotifyError string

func (e DiscordNotifyError) Error() string {
	return "error notifying discord: " + string(e)
}

type DiscordErrorNotifyError struct {
	toSend error
	found  error
}

func (e DiscordErrorNotifyError) Error() string {
	return "error notifying error (" + e.toSend.Error() + ") to discord: " + e.found.Error()
}

func chaptersToList(chapters chapter.Chapters, includeDirs bool) (out string) {
	var lastDir string
	for i, ch := range chapters {
		if ch.Down != nil &&
			ch.Down.ChapterStatus == metadata.DownloadStatusExists &&
			!config.Notification.IncludeExisting.Get() {
			continue
		}
		if includeDirs &&
			ch.Down != nil &&
			ch.Down.Directory != lastDir {
			lastDir = ch.Down.Directory
			out += lastDir
			out += ":\n"
		}
		out += "> "
		if ch.Down != nil {
			switch ch.Down.ChapterStatus {
			case metadata.DownloadStatusNew:
				out += "[N]"
			case metadata.DownloadStatusMissingMetadata:
				out += "[M]"
			case metadata.DownloadStatusOverwritten:
				out += "[O]"
			case metadata.DownloadStatusSkip:
				out += "[S]"
			case metadata.DownloadStatusFailed:
				out += "[F]"
			case metadata.DownloadStatusExists:
				out += "[E]"
			default:
				out += "[?]"
			}
			out += " "
		}
		out += stringutil.FormatFloa32(ch.Chapter.Info().Number)
		out += " - "
		out += ch.Chapter.Info().Title
		if i < len(chapters) {
			out += "\n"
		}
	}
	return
}

// Send will send a message using the configured discord WebhookURL.
func Send(chapters chapter.Chapters) error {
	if len(chapters) == 0 {
		return SendError(errors.New("No downloaded chapters to be notified of."))
	}
	d, s, f := chapters.GetEach()
	e := s.Existent()

	// All downloaded chapters are succeed already existent
	if len(chapters) == len(e) && !config.Notification.IncludeExisting.Get() {
		return nil
	}

	client, err := webhook.NewWithURL(config.Notification.Discord.WebhookURL.Get())
	if err != nil {
		return DiscordNotifyError(err.Error())
	}

	var (
		color      int
		failed     string
		succeed    string
		toDownload string
		fields     []discord.EmbedField
	)

	switch {
	case len(f) != 0:
		color = ColorRed
	case len(d) != 0:
		color = ColorOrange
	default:
		color = ColorGreen
	}

	succeedCount := strconv.Itoa(len(s))
	if len(e) != 0 {
		succeedCount += " (" + strconv.Itoa(len(e)) + " existed)"
	}

	// summaries
	inline := true
	fields = append(fields, discord.EmbedField{
		Name:   "succeed",
		Value:  succeedCount,
		Inline: &inline,
	})
	fields = append(fields, discord.EmbedField{
		Name:   "failed",
		Value:  strconv.Itoa(len(f)),
		Inline: &inline,
	})
	fields = append(fields, discord.EmbedField{
		Name:   "to download",
		Value:  strconv.Itoa(len(d)),
		Inline: &inline,
	})

	// failed
	if len(f) != 0 {
		failed = chaptersToList(f, false)
		fields = append(fields, discord.EmbedField{
			Name:  "failed",
			Value: failed,
		})
	}

	// succeed
	if len(s) != 0 {
		succeed = chaptersToList(s, config.Notification.IncludeDirectory.Get())
		fields = append(fields, discord.EmbedField{
			Name:  "succeed",
			Value: succeed,
		})
	}

	// to download
	if len(d) != 0 {
		toDownload = chaptersToList(d, false)
		fields = append(fields, discord.EmbedField{
			Name:  "to download",
			Value: toDownload,
		})
	}

	t := time.Now()
	message, err := client.CreateMessage(discord.WebhookMessageCreate{
		Username: config.Notification.Discord.Username.Get(),
		Embeds: []discord.Embed{
			{
				Title:       "Downloaded chapters",
				Description: chapters[0].Chapter.Volume().Manga().Info().Title,
				Color:       color,
				Timestamp:   &t,
				Fields:      fields,
			},
		},
	},
		rest.CreateWebhookMessageParams{})
	if err != nil {
		return DiscordNotifyError(err.Error())
	}
	log.Log("sent discord message with id %d", message.ID)
	return nil
}

// SendError is a wrapper error handling that will send a webhook
// message to the configured discord WebhookURL and return the same error back.
func SendError(toSend error) error {
	client, err := webhook.NewWithURL(config.Notification.Discord.WebhookURL.Get())
	if err != nil {
		return DiscordErrorNotifyError{toSend, err}
	}

	t := time.Now()
	message, err := client.CreateMessage(discord.WebhookMessageCreate{
		Username: config.Notification.Discord.Username.Get(),
		Embeds: []discord.Embed{
			{
				Title: "Error",
				Description: "General error found when downloading chapters:\n```" +
					toSend.Error() + "```",
				Color:     ColorRed,
				Timestamp: &t,
			},
		},
	},
		rest.CreateWebhookMessageParams{})
	if err != nil {
		return DiscordErrorNotifyError{toSend, err}
	}
	log.Log("sent discord error message with id %d", message.ID)
	return toSend
}
