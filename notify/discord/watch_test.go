package discord

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
)

func TestWatchEmbed(t *testing.T) {
	from := float32(65)
	r := watch.Result{Provider: "mango-mangaplus", Title: "Dandadan", From: &from, Status: storage.StatusNew,
		Chapters: []storage.RunChapter{{Number: 65.5, Title: "extra", Path: "/lib/Dandadan/a.cbz"}, {Number: 70, Path: "/lib/Dandadan/b.cbz"}}}
	e := watchEmbed(r, true)
	if e.Description != "chapters 65 → 70: 2 new (from 65)" || e.Title != "Dandadan" || e.Color != ColorGreen {
		t.Errorf("embed %q %q %d", e.Title, e.Description, e.Color)
	}
	last := e.Fields[len(e.Fields)-1]
	if last.Name != "chapters" || last.Value != "/lib/Dandadan:\n> 65.5 - extra\n> 70" {
		t.Errorf("chapters field %q", last.Value)
	}

	r.From, r.Error = nil, "chapter 71: boom"
	if e := watchEmbed(r, false); e.Description != "chapters → 70: 2 new (first check)" || e.Color != ColorOrange || e.Fields[len(e.Fields)-1].Name != "failed" {
		t.Errorf("first check with a failure: %+v", e)
	}
	if e := watchErrorEmbed(watch.Result{Provider: "p", Title: "X", Error: strings.Repeat("e", 5000)}); utf8.RuneCountInString(e.Description) > maxDescription || e.Color != ColorRed {
		t.Errorf("error embed too long or not red: %d", len(e.Description))
	}
}

func TestSendWithoutMessageBack(t *testing.T) {
	// Discord answers 204 without wait: no message, and that's not an error
	saved := createMessage
	t.Cleanup(func() { createMessage = saved })
	var got discord.WebhookMessageCreate
	createMessage = func(_ context.Context, _ string, m discord.WebhookMessageCreate) (*discord.Message, error) {
		got = m
		return nil, nil
	}
	r := watch.Result{Provider: "p", Title: "X", Status: storage.StatusNew, Chapters: []storage.RunChapter{{Number: 1}}}
	if err := SendWatch(context.Background(), "https://discord.com/api/webhooks/1/x", r, false); err != nil {
		t.Fatal(err)
	}
	if err := SendWatchError(context.Background(), "https://discord.com/api/webhooks/1/x", r); err != nil {
		t.Fatal(err)
	}
	if len(got.Embeds) != 1 || got.Embeds[0].Timestamp == nil {
		t.Errorf("sent %+v", got)
	}
}

func TestSendRedactsWebhook(t *testing.T) {
	saved := createMessage
	t.Cleanup(func() { createMessage = saved })
	const hook = "https://discord.com/api/webhooks/1/s3cr3tt0ken"
	for _, sendErr := range []error{
		&url.Error{Op: "Post", URL: "https://discord.com/api/v10/webhooks/1/s3cr3tt0ken?wait=true", Err: context.DeadlineExceeded},
		errors.New("bad webhook " + hook),
	} {
		createMessage = func(context.Context, string, discord.WebhookMessageCreate) (*discord.Message, error) {
			return nil, sendErr
		}
		r := watch.Result{Provider: "p", Title: "X", Status: storage.StatusNew, Chapters: []storage.RunChapter{{Number: 1}}}
		err := SendWatch(context.Background(), hook, r, false)
		if err == nil || strings.Contains(err.Error(), "s3cr3tt0ken") {
			t.Errorf("error %v", err)
		}
		var ue *url.Error
		if errors.As(sendErr, &ue) && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%v isn't the deadline", err)
		}
	}
}
