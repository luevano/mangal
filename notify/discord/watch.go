package discord

import (
	"context"
	"fmt"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/luevano/mangal/v5/client/watch"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// watchSummary is "chapters 65 → 70: 5 new (from 65)", from being the last downloaded
// chapter before the check.
func watchSummary(r watch.Result) string {
	newest := r.Newest()
	if newest == nil {
		return "no new chapters"
	}
	if r.From == nil {
		return fmt.Sprintf("chapters → %s: %d new (first check)", stringutil.FormatFloa32(*newest), len(r.Chapters))
	}
	from := stringutil.FormatFloa32(*r.From)
	return fmt.Sprintf("chapters %s → %s: %d new (from %s)", from, stringutil.FormatFloa32(*newest), len(r.Chapters), from)
}

// watchEmbed is the message of a watch that downloaded chapters.
func watchEmbed(r watch.Result, includeDirs bool) discord.Embed {
	var lines []string
	var lastDir string
	for _, ch := range r.Chapters {
		if dir := dirOf(ch.Path); includeDirs && dir != lastDir {
			lastDir = dir
			lines = append(lines, dir+":")
		}
		line := "> " + stringutil.FormatFloa32(ch.Number)
		if ch.Title != "" {
			line += " - " + ch.Title
		}
		lines = append(lines, line)
	}
	inline := true
	fields := []discord.EmbedField{
		{Name: "provider", Value: r.Provider, Inline: &inline},
		{Name: "new", Value: fmt.Sprint(len(r.Chapters)), Inline: &inline},
	}
	if len(lines) != 0 {
		fields = append(fields, discord.EmbedField{Name: "chapters", Value: truncate(strings.Join(lines, "\n"), maxFieldValue)})
	}
	color := ColorGreen
	if r.Error != "" {
		color = ColorOrange
		fields = append(fields, discord.EmbedField{Name: "failed", Value: truncate(r.Error, maxFieldValue)})
	}
	return discord.Embed{
		Title:       truncate(r.Title, 256),
		Description: watchSummary(r),
		Color:       color,
		Fields:      fields,
	}
}

func dirOf(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[:i]
	}
	return ""
}

// watchErrorEmbed is the message of a watch whose check failed.
func watchErrorEmbed(r watch.Result) discord.Embed {
	return discord.Embed{
		Title:       truncate("Error: "+r.Title, 256),
		Description: truncate(fmt.Sprintf("%s (%s):\n```%s", r.Title, r.Provider, r.Error), maxDescription-3) + "```",
		Color:       ColorRed,
	}
}

// SendWatch sends the chapters a watch downloaded to the webhook.
func SendWatch(ctx context.Context, url string, r watch.Result, includeDirs bool) error {
	if err := send(ctx, url, watchEmbed(r, includeDirs), "message"); err != nil {
		return fmt.Errorf("notify discord: %w", err)
	}
	return nil
}

// SendWatchError sends a watch's failed check to the webhook.
func SendWatchError(ctx context.Context, url string, r watch.Result) error {
	if err := send(ctx, url, watchErrorEmbed(r), "error message"); err != nil {
		return fmt.Errorf("notify discord: %w", err)
	}
	return nil
}
