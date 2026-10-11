package mangas

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/tui/model/metadata"
)

var (
	_ list.Item        = (*item)(nil)
	_ list.DefaultItem = (*item)(nil)
)

// item implements list.item.
type item struct {
	manga mangadata.Manga
	meta  *metadata.Model

	extraInfo     *bool
	fullExtraInfo *bool

	renderedMeta     string
	renderedFullMeta string
}

// FilterValue implements list.Item.
func (i *item) FilterValue() string {
	return i.manga.String()
}

// Title implements list.DefaultItem.
func (i *item) Title() string {
	if !(*i.extraInfo) {
		return i.FilterValue()
	}

	if *i.fullExtraInfo {
		return i.FilterValue() + i.renderedFullMeta
	}
	return i.FilterValue() + i.renderedMeta
}

// Description implements list.DefaultItem.
func (i *item) Description() string {
	return i.manga.Info().URL
}

func newItem(manga mangadata.Manga, info, fullInfo *bool) *item {
	i := &item{
		manga:         manga,
		meta:          metadata.New(manga.Metadata()),
		extraInfo:     info,
		fullExtraInfo: fullInfo,
	}
	i.renderMetadata()
	return i
}

// updateMetadata re-sets the metadata to the metadata viewer.
func (i *item) updateMetadata() {
	i.meta.SetMetadata(i.manga.Metadata())
	i.renderMetadata()
}

// renderMetadata pre-renders the metadata for later use.
func (i *item) renderMetadata() {
	i.meta.ShowFull = false
	i.renderedMeta = " " + i.meta.View()
	i.meta.ShowFull = true
	i.renderedFullMeta = " " + i.meta.View()
}
