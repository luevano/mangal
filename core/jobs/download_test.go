package jobs

import (
	"context"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/logger"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client"
)

type fakeLoader struct{}

func (fakeLoader) String() string { return "p" }
func (fakeLoader) Info() libmangal.ProviderInfo {
	return libmangal.ProviderInfo{ID: "p", Name: "P", Version: "0.1.0"}
}
func (fakeLoader) Load(context.Context) (libmangal.Provider, error) { return fakeProvider{}, nil }

type fakeProvider struct{ libmangal.Provider }

func (fakeProvider) Info() libmangal.ProviderInfo { return fakeLoader{}.Info() }
func (fakeProvider) SetLogger(*logger.Logger)     {}

type fakeManga struct{ mangadata.Manga }

func (fakeManga) Info() mangadata.MangaInfo { return mangadata.MangaInfo{ID: "m", Title: "M"} }

// TestDownloadHoldsClient: the job downloads with the provider's client held while it runs
// (a reload can't close it), not with the one it was made with.
func TestDownloadHoldsClient(t *testing.T) {
	ctx := context.Background()
	made, err := libmangal.NewClientContext(ctx, fakeLoader{}, libmangal.DefaultClientOptions())
	if err != nil {
		t.Fatal(err)
	}
	held, err := libmangal.NewClientContext(ctx, fakeLoader{}, libmangal.DefaultClientOptions())
	if err != nil {
		t.Fatal(err)
	}
	released := 0
	oldHold, oldDownload := holdClient, downloadChapters
	t.Cleanup(func() { holdClient, downloadChapters = oldHold, oldDownload })
	holdClient = func(_ context.Context, id string) (*libmangal.Client, []string, func(), error) {
		if id != "p" {
			t.Errorf("held %q", id)
		}
		return held, nil, func() { released++ }, nil
	}
	downloadChapters = func(ctx context.Context, c *libmangal.Client, list []mangadata.Chapter, _ libmangal.DownloadOptions, onEvent func(client.Event)) client.Results {
		if c != held {
			t.Error("downloads with the client it was made with")
		}
		if released != 0 {
			t.Error("released while downloading")
		}
		return fakeDownload(nil)(ctx, list, onEvent)
	}
	m := newManager(t)
	j := m.Add(DownloadSpec(made, fakeManga{}, chapters(1, 2), libmangal.DownloadOptions{}, "M", "test"))
	waitFor(t, m, j.ID, "done", status(Done))
	if released != 1 {
		t.Errorf("released %d times", released)
	}
}
