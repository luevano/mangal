package watches

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/logger"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client/watch"
)

type byIDLoader struct{}

func (byIDLoader) String() string { return "byid" }
func (byIDLoader) Info() libmangal.ProviderInfo {
	return libmangal.ProviderInfo{ID: "byid", Name: "ByID", Version: "0.1.0"}
}
func (byIDLoader) Load(context.Context) (libmangal.Provider, error) { return byIDProvider{}, nil }

// byIDProvider has the manga "here", the site is down for "down".
type byIDProvider struct{ libmangal.Provider }

type byIDManga struct{ mangadata.Manga }

func (byIDProvider) Info() libmangal.ProviderInfo { return byIDLoader{}.Info() }
func (byIDProvider) String() string               { return "ByID" }
func (byIDProvider) SetLogger(*logger.Logger)     {}
func (byIDProvider) Close() error                 { return nil }
func (byIDProvider) MangaByID(_ context.Context, id string) (mangadata.Manga, bool, error) {
	switch id {
	case "here":
		return byIDManga{}, true, nil
	case "down":
		return nil, false, errors.New("site down")
	}
	return nil, false, nil
}

// TestFindManga: a library source's manga the provider doesn't have any more is said as
// such, another error with its reason.
func TestFindManga(t *testing.T) {
	c, err := libmangal.NewClient(byIDLoader{}, libmangal.DefaultClientOptions())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := findManga(ctx, c, "here", "Here"); err != nil {
		t.Errorf("here: %v", err)
	}
	_, err = findManga(ctx, c, "gone", "Berserk")
	if !errors.Is(err, watch.ErrMangaNotFound) || err.Error() != `manga not found: "Berserk" isn't on ByID any more, it may have been removed (manga id gone)` {
		t.Errorf("gone: %v", err)
	}
	if _, err := findManga(ctx, c, "down", "Down"); errors.Is(err, watch.ErrMangaNotFound) || !strings.HasPrefix(err.Error(), `look up "Down" on ByID: `) || !strings.HasSuffix(err.Error(), "site down") {
		t.Errorf("down: %v", err)
	}
}
