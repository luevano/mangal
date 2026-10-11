package metadata

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/list"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/icon"
)

type styles struct {
	enum func(list.Items, int) string

	fieldName,
	enumerator lipgloss.Style
}

func newStyles(meta metaStyle) styles {
	base := lipgloss.NewStyle().Foreground(meta.Color)
	return styles{
		enum: func(_ list.Items, _ int) string {
			return icon.SubItem.Raw()
		},
		fieldName:  base,
		enumerator: base.MarginRight(1).PaddingLeft(2),
	}
}

type metaStyle struct {
	Color  lipgloss.Color
	Prefix string
	Code   string
}

func metaIDStyle(id metadata.ID) metaStyle {
	c := color.Provider
	p := "Provider"
	v := id.Code
	switch id.Source {
	case metadata.IDSourceAnilist:
		c = color.Anilist
		p = "Anilist"
		v = metadata.IDCodeAnilist
	case metadata.IDSourceMyAnimeList:
		c = color.MyAnimeList
		p = "MyAnimeList"
		v = metadata.IDCodeMyAnimeList
	case metadata.IDSourceKitsu:
		c = color.Kitsu
		p = "Kitsu"
		v = metadata.IDCodeKitsu
	case metadata.IDSourceMangaUpdates:
		c = color.MangaUpdates
		p = "MangaUpdates"
		v = metadata.IDCodeMangaUpdates
	case metadata.IDSourceAnimePlanet:
		c = color.AnimePlanet
		p = "AnimePlanet"
		v = metadata.IDCodeAnimePlanet
	}
	return metaStyle{c, p, string(v)}
}
