package anilist

import (
	"testing"

	"github.com/luevano/libmangal/metadata/anilist"
	"github.com/luevano/mangal/v5/config"
)

func TestOptionsTagRankMin(t *testing.T) {
	if got := anilistOptions().TagRankMin; got != anilist.DefaultTagRankMin {
		t.Errorf("default: %d", got)
	}
	old := config.Metadata.Anilist.TagRankMin.Get()
	t.Cleanup(func() { _ = config.Metadata.Anilist.TagRankMin.Set(old) })
	if err := config.Metadata.Anilist.TagRankMin.Set(30); err != nil {
		t.Fatal(err)
	}
	if got := anilistOptions().TagRankMin; got != 30 {
		t.Errorf("set to 30: %d", got)
	}
}
