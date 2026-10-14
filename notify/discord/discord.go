// Package discord sends notifications to a Discord webhook (notification.discord.*).
package discord

import (
	"context"
	"errors"
	"fmt"
	neturl "net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/disgo/webhook"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

const (
	ColorRed    = 15548997
	ColorOrange = 15105570
	ColorGreen  = 5763719
)

// Discord rejects the whole message if a limit is passed
const (
	maxFieldValue  = 1024
	maxDescription = 4096
)

// listOptions are the config of the chapter lists.
type listOptions struct {
	includeExisting bool
	includeDirs     bool
}

func chaptersToList(chapters client.Results, opts listOptions) string {
	var lines []string
	var lastDir string
	for _, ch := range chapters {
		if ch.Down != nil && ch.Down.ChapterStatus == metadata.DownloadStatusExists && !opts.includeExisting {
			continue
		}
		if opts.includeDirs && ch.Down != nil && ch.Down.Directory != lastDir {
			lastDir = ch.Down.Directory
			lines = append(lines, lastDir+":")
		}
		line := "> "
		if ch.Down != nil {
			line += status(ch.Down.ChapterStatus) + " "
		}
		info := ch.Chapter.Info()
		line += stringutil.FormatFloat32(info.Number) + " - " + info.Title
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func status(s metadata.DownloadStatus) string {
	switch s {
	case metadata.DownloadStatusNew:
		return "[N]"
	case metadata.DownloadStatusMissingMetadata:
		return "[M]"
	case metadata.DownloadStatusOverwritten:
		return "[O]"
	case metadata.DownloadStatusSkip:
		return "[S]"
	case metadata.DownloadStatusFailed:
		return "[F]"
	case metadata.DownloadStatusExists:
		return "[E]"
	default:
		return "[?]"
	}
}

// truncate cuts s to max runes, marking the cut.
func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	const more = "\n…"
	runes := []rune(s)
	return string(runes[:max-utf8.RuneCountInString(more)]) + more
}

// embed builds the message of the download, false if there's nothing to notify.
func embed(results client.Results, opts listOptions) (discord.Embed, bool) {
	if len(results) == 0 {
		return discord.Embed{
			Title:       "Downloaded chapters",
			Description: "No chapters to download.",
			Color:       ColorOrange,
		}, true
	}
	d, s, f := results.GetEach()
	e := s.Existent()
	// every chapter already existed
	if len(results) == len(e) && !opts.includeExisting {
		return discord.Embed{}, false
	}

	color := ColorGreen
	switch {
	case len(f) != 0:
		color = ColorRed
	case len(d) != 0:
		color = ColorOrange
	}

	succeedCount := strconv.Itoa(len(s))
	if len(e) != 0 {
		succeedCount += " (" + strconv.Itoa(len(e)) + " existed)"
	}
	inline := true
	fields := []discord.EmbedField{
		{Name: "succeed", Value: succeedCount, Inline: &inline},
		{Name: "failed", Value: strconv.Itoa(len(f)), Inline: &inline},
		{Name: "to download", Value: strconv.Itoa(len(d)), Inline: &inline},
	}
	addList := func(name string, chapters client.Results, opts listOptions) {
		// an empty value is rejected (e.g. every succeeded chapter existed)
		if list := chaptersToList(chapters, opts); list != "" {
			fields = append(fields, discord.EmbedField{Name: name, Value: truncate(list, maxFieldValue)})
		}
	}
	addList("failed", f, listOptions{includeExisting: opts.includeExisting})
	addList("succeed", s, opts)
	addList("to download", d, listOptions{includeExisting: opts.includeExisting})

	return discord.Embed{
		Title:       "Downloaded chapters",
		Description: truncate(results[0].Chapter.Volume().Manga().Info().Title, maxDescription),
		Color:       color,
		Fields:      fields,
	}, true
}

// createMessage posts the message to the webhook, swapped in tests. Wait makes Discord
// answer with the message (without it there's no body, the message is nil).
var createMessage = func(ctx context.Context, url string, m discord.WebhookMessageCreate) (*discord.Message, error) {
	c, err := webhook.NewWithURL(url)
	if err != nil {
		return nil, err
	}
	return c.CreateMessage(m, rest.CreateWebhookMessageParams{Wait: true}, rest.WithCtx(ctx))
}

// send posts the embed to the webhook and logs it, what says what it was.
func send(ctx context.Context, url string, e discord.Embed, what string) error {
	t := time.Now()
	e.Timestamp = &t
	m, err := createMessage(ctx, url, discord.WebhookMessageCreate{
		Username: config.Notification.Discord.Username.Get(),
		Embeds:   []discord.Embed{e},
	})
	if err != nil {
		return redact(err, url)
	}
	if m != nil {
		log.Info("sent discord %s with id %d", what, m.ID)
	} else {
		log.Info("sent discord %s", what)
	}
	return nil
}

// redact drops the webhook URL from the error: its token is the secret, and the errors of
// the requests (*url.Error) carry it to the logs.
func redact(err error, webhook string) error {
	var ue *neturl.Error
	if errors.As(err, &ue) {
		err = fmt.Errorf("%s discord webhook: %w", ue.Op, ue.Err)
	}
	// disgo builds its own url from the webhook's id and token
	token := webhook[strings.LastIndex(webhook, "/")+1:]
	if msg := err.Error(); token != "" && strings.Contains(msg, token) {
		err = errors.New(strings.ReplaceAll(strings.ReplaceAll(msg, webhook, "<webhook url>"), token, "<token>"))
	}
	return err
}

// Send sends a summary of the download to the webhook.
func Send(ctx context.Context, url string, results client.Results) error {
	e, ok := embed(results, listOptions{
		includeExisting: config.Notification.IncludeExisting.Get(),
		includeDirs:     config.Notification.IncludeDirectory.Get(),
	})
	if !ok {
		return nil
	}
	if err := send(ctx, url, e, "message"); err != nil {
		return fmt.Errorf("notify discord: %w", err)
	}
	return nil
}

// SendError sends the error to the webhook and returns it, joined with the error sending
// it if any.
func SendError(ctx context.Context, url string, toSend error) error {
	err := send(ctx, url, discord.Embed{
		Title:       "Error",
		Description: truncate("General error found when downloading chapters:\n```"+toSend.Error(), maxDescription-3) + "```",
		Color:       ColorRed,
	}, "error message")
	if err != nil {
		return errors.Join(toSend, fmt.Errorf("notify discord: %w", err))
	}
	return toSend
}
