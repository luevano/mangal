package mangas

import (
	_list "github.com/charmbracelet/bubbles/list"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/model/search"
)

func New(client *libmangal.Client) *state {
	info := false
	fullInfo := false
	listWrapper := list.New(
		2, 1,
		"manga", "mangas",
		nil,
		func(manga mangadata.Manga) _list.DefaultItem {
			return newItem(manga, &info, &fullInfo)
		},
	)

	s := &state{
		list:          listWrapper,
		search:        search.New("Search manga...", "", 64, 5),
		client:        client,
		extraInfo:     &info,
		fullExtraInfo: &fullInfo,
		keyMap:        newKeyMap(),
	}
	s.updateKeybinds()
	return s
}
