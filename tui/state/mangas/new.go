package mangas

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/tui/state/listwrapper"
	"github.com/luevano/mangal/tui/util"
)

func New(client *libmangal.Client, query string, mangas []mangadata.Manga) *State {
	listWrapper := listwrapper.New(util.NewList(
		2,
		"manga", "mangas",
		mangas,
		func(manga mangadata.Manga) list.DefaultItem {
			return Item{manga}
		},
	))

	return &State{
		list:   listWrapper,
		mangas: mangas,
		client: client,
		query:  query,
		keyMap: keyMap{
			confirm: util.Bind("confirm", "enter"),
			list:    listWrapper.KeyMap(),
		},
	}
}
