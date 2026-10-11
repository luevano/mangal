package mangas

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/tui/model/metadata"
)

var _ list.DefaultItem = (*item)(nil)

type item struct {
	manga mangadata.Manga

	extraInfo     *bool
	fullExtraInfo *bool

	renderedMeta     string
	renderedFullMeta string
}

func (i *item) FilterValue() string {
	return i.manga.String()
}

func (i *item) Title() string {
	switch {
	case !*i.extraInfo:
		return i.FilterValue()
	case *i.fullExtraInfo:
		return i.FilterValue() + i.renderedFullMeta
	default:
		return i.FilterValue() + i.renderedMeta
	}
}

func (i *item) Description() string {
	return i.manga.Info().URL
}

func newItem(manga mangadata.Manga, info, fullInfo *bool) *item {
	i := &item{
		manga:         manga,
		extraInfo:     info,
		fullExtraInfo: fullInfo,
	}
	i.renderMetadata()
	return i
}

// renderMetadata pre-renders the metadata badges, again after it changes.
func (i *item) renderMetadata() {
	i.renderedMeta, i.renderedFullMeta = "", ""
	if meta := i.manga.Metadata(); meta != nil {
		i.renderedMeta = " " + metadata.Badge(meta, false)
		i.renderedFullMeta = " " + metadata.Badge(meta, true)
	}
}
