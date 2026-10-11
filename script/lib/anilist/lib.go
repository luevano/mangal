package anilist

import (
	"fmt"

	luadoc "github.com/luevano/gopher-luadoc"
	lmanilist "github.com/luevano/libmangal/metadata/anilist"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/script/lib/util"
	lua "github.com/yuin/gopher-lua"
)

const (
	libName = "anilist"

	mangaTypeName = libName + "_manga"
)

func Lib() *luadoc.Lib {
	classManga := &luadoc.Class{
		Name:        mangaTypeName,
		Description: "Anilist manga",
		Methods: []*luadoc.Method{
			{
				Name:        "info",
				Description: "Manga information",
				Value:       mangaInfo,
				Returns: []*luadoc.Param{
					{
						Name: "info",
						Type: luadoc.TableLiteral(
							"id", luadoc.Number,
							"title", luadoc.String,
							"alternate_titles", luadoc.List(luadoc.String),
							"description", luadoc.String,
							"url", luadoc.String,
							"cover", luadoc.String,
							"banner", luadoc.String,
							"score", luadoc.Number,
							"status", luadoc.String,
							"year", luadoc.Number,
							"chapters", luadoc.Number,
							"genres", luadoc.List(luadoc.String),
						),
					},
				},
			},
		},
	}

	return &luadoc.Lib{
		Name:        libName,
		Description: "Anilist operations",
		Classes:     []*luadoc.Class{classManga},
		Funcs: []*luadoc.Func{
			{
				Name:        "search_mangas",
				Description: "Search mangas on Anilist",
				Value:       searchMangas,
				Params: []*luadoc.Param{
					{
						Name:        "query",
						Description: "Query to search",
						Type:        luadoc.String,
					},
				},
				Returns: []*luadoc.Param{
					{
						Name:        "mangas",
						Description: "Anilist mangas",
						Type:        luadoc.List(mangaTypeName),
					},
				},
			},
			{
				Name:        "find_closest_mangas",
				Description: "Find the Anilist manga closest to the title, bad matches aren't returned",
				Value:       findClosestManga,
				Params: []*luadoc.Param{
					{
						Name:        "title",
						Description: "Manga title to search for",
						Type:        luadoc.String,
					},
				},
				Returns: []*luadoc.Param{
					{
						Name:        "manga",
						Description: "Closest manga found, nil if none",
						Type:        mangaTypeName,
					},
					{
						Name:        "found",
						Description: "Whether the closest manga was found",
						Type:        luadoc.Boolean,
					},
				},
			},
			{
				Name:        "bind_title_with_id",
				Description: "Binds manga title to anilist manga id",
				Value:       bindTitleWithID,
				Params: []*luadoc.Param{
					{
						Name:        "title",
						Description: "Manga title to use for binding",
						Type:        luadoc.String,
					},
					{
						Name:        "id",
						Description: "Anilist manga ID to bind to",
						Type:        luadoc.Number,
					},
				},
			},
		},
	}
}

func mangaInfo(state *lua.LState) int {
	manga := util.Check[*lmanilist.Manga](state, 1)

	table := state.NewTable()
	table.RawSetString("id", lua.LNumber(manga.IDProvider))
	table.RawSetString("title", lua.LString(manga.Title()))
	table.RawSetString("alternate_titles", util.SliceToTable(state, manga.AlternateTitles(), func(s string) lua.LValue {
		return lua.LString(s)
	}))
	table.RawSetString("description", lua.LString(manga.Description()))
	table.RawSetString("url", lua.LString(manga.URL()))
	table.RawSetString("cover", lua.LString(manga.Cover()))
	table.RawSetString("banner", lua.LString(manga.Banner()))
	table.RawSetString("score", lua.LNumber(manga.Score()))
	table.RawSetString("status", lua.LString(manga.Status()))
	table.RawSetString("year", lua.LNumber(manga.StartDate().Year))
	table.RawSetString("chapters", lua.LNumber(manga.Chapters()))
	table.RawSetString("genres", util.SliceToTable(state, manga.Genres(), func(s string) lua.LValue {
		return lua.LString(s)
	}))

	state.Push(table)
	return 1
}

func searchMangas(state *lua.LState) int {
	query := state.CheckString(1)

	ani, err := anilist.Anilist()
	util.Must(state, err)
	_ = anilist.EnsureLogin(state.Context())

	metas, err := ani.SearchContext(state.Context(), query)
	util.Must(state, err)

	table := state.NewTable()
	for _, m := range metas {
		manga, ok := m.(*lmanilist.Manga)
		if !ok {
			util.Must(state, fmt.Errorf("unexpected anilist result %T", m))
		}
		table.Append(util.NewUserData(state, manga, mangaTypeName))
	}

	state.Push(table)
	return 1
}

func findClosestManga(state *lua.LState) int {
	title := state.CheckString(1)

	manga, _, found, err := anilist.FindClosest(state.Context(), title)
	util.Must(state, err)

	if !found {
		state.Push(lua.LNil)
		state.Push(lua.LFalse)
		return 2
	}
	util.Push(state, manga, mangaTypeName)
	state.Push(lua.LTrue)
	return 2
}

func bindTitleWithID(state *lua.LState) int {
	title := state.CheckString(1)
	id := state.CheckInt(2)

	ani, err := anilist.Anilist()
	util.Must(state, err)
	util.Must(state, ani.BindTitleWithID(title, id))

	return 0
}
