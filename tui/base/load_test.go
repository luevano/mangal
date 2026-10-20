package base

import (
	"errors"
	"fmt"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client/watch"
)

type goneManga struct{ mangadata.Manga }

func (goneManga) Info() mangadata.MangaInfo { return mangadata.MangaInfo{ID: "gone", Title: "Berserk"} }

// a provider's not found is the manga gone, other errors stay
func TestMangaError(t *testing.T) {
	err := MangaError(fmt.Errorf("get the volumes: %w", libmangal.ErrNotFound), "MangaDex", goneManga{})
	if !errors.Is(err, watch.ErrMangaNotFound) || err.Error() != `manga not found: "Berserk" isn't on MangaDex any more, it may have been removed (manga id gone)` {
		t.Errorf("gone: %v", err)
	}
	other := errors.New("site down")
	if err := MangaError(other, "MangaDex", goneManga{}); err != other {
		t.Errorf("other: %v", err)
	}
	if MangaError(nil, "MangaDex", goneManga{}) != nil {
		t.Error("nil")
	}
}
