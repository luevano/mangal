package anilist

import (
	_list "github.com/charmbracelet/bubbles/list"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/model/search"
)

func New(anilist *metadata.ProviderWithCache, manga mangadata.Manga) *state {
	listWrapper := list.New(
		2, 1,
		"anilist manga", "anilist mangas",
		nil,
		func(manga metadata.Metadata) _list.DefaultItem {
			return &item{meta: manga}
		},
	)

	title := manga.Info().Title
	s := &state{
		anilist: anilist,
		search:  search.New("Search anilist manga...", title, 64, 5),
		manga:   manga,
		list:    listWrapper,
		keyMap:  newKeyMap(),
	}
	s.updateKeybinds()
	return s
}
