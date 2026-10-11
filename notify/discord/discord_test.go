package discord

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
)

type fakeManga struct{ mangadata.Manga }

func (fakeManga) Info() mangadata.MangaInfo { return mangadata.MangaInfo{Title: "Berserk"} }

type fakeVolume struct{ mangadata.Volume }

func (fakeVolume) Manga() mangadata.Manga { return fakeManga{} }

type fakeChapter struct {
	mangadata.Chapter
	number float32
	title  string
}

func (c fakeChapter) Info() mangadata.ChapterInfo {
	return mangadata.ChapterInfo{Number: c.number, Title: c.title}
}
func (fakeChapter) Volume() mangadata.Volume { return fakeVolume{} }

func result(number float32, title string, status metadata.DownloadStatus, dir string) *client.Result {
	r := &client.Result{Chapter: fakeChapter{number: number, title: title}}
	switch status {
	case "":
	case metadata.DownloadStatusFailed:
		r.Err = errors.New("boom")
	default:
		r.Down = &metadata.DownloadedChapter{ChapterStatus: status, Directory: dir}
	}
	return r
}

func TestChaptersToList(t *testing.T) {
	results := client.Results{
		result(1, "A", metadata.DownloadStatusNew, "/dl/Berserk"),
		result(2, "B", metadata.DownloadStatusExists, "/dl/Berserk"),
		result(10.1, "C", metadata.DownloadStatusNew, "/dl/Berserk"),
	}
	got := chaptersToList(results, listOptions{includeDirs: true})
	want := "/dl/Berserk:\n> [N] 1 - A\n> [N] 10.1 - C"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if got := chaptersToList(results, listOptions{includeExisting: true}); !strings.Contains(got, "[E] 2 - B") {
		t.Errorf("existing missing:\n%s", got)
	}
}

func TestEmbed(t *testing.T) {
	// every chapter existed: nothing to send
	existed := client.Results{result(1, "A", metadata.DownloadStatusExists, "/dl")}
	if _, ok := embed(existed, listOptions{}); ok {
		t.Error("sent although every chapter existed")
	}
	// existed ones hidden: no empty succeed list (discord rejects empty values)
	mixed := client.Results{
		result(1, "A", metadata.DownloadStatusExists, "/dl"),
		result(2, "B", metadata.DownloadStatusFailed, ""),
	}
	e, ok := embed(mixed, listOptions{})
	if !ok {
		t.Fatal("not sent")
	}
	for _, f := range e.Fields {
		if f.Value == "" {
			t.Errorf("empty field %q", f.Name)
		}
	}
	if e.Color != ColorRed || e.Description != "Berserk" {
		t.Errorf("color %d description %q", e.Color, e.Description)
	}

	if _, ok := embed(nil, listOptions{}); !ok {
		t.Error("no message for no chapters")
	}
}

func TestEmbedFieldLimit(t *testing.T) {
	var results client.Results
	for i := range 200 {
		results = append(results, result(float32(i), strings.Repeat("título ", 5), metadata.DownloadStatusNew, "/dl"))
	}
	e, _ := embed(results, listOptions{})
	for _, f := range e.Fields {
		if n := utf8.RuneCountInString(f.Value); n > maxFieldValue {
			t.Errorf("field %q has %d chars", f.Name, n)
		}
	}
}
