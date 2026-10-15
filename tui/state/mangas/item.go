package mangas

import (
	"charm.land/bubbles/v2/list"
	"github.com/luevano/libmangal/mangadata"
	tuijobs "github.com/luevano/mangal/v5/tui/jobs"
	"github.com/luevano/mangal/v5/tui/model/metadata"
	"github.com/luevano/mangal/v5/tui/state/watches"
)

var _ list.DefaultItem = (*item)(nil)

type item struct {
	manga mangadata.Manga
	// watched marks it
	watched bool
	// provider is the client's, for the mark of a download job of the manga
	provider string

	extraInfo     *bool
	fullExtraInfo *bool

	renderedMeta     string
	renderedFullMeta string
}

func (i *item) FilterValue() string {
	return i.manga.String()
}

func (i *item) Title() string {
	title := i.FilterValue()
	if mark := tuijobs.MangaMark(i.provider, i.manga.Info().ID); mark != "" {
		title += " " + mark
	}
	if i.watched {
		title += " " + watches.Mark()
	}
	switch {
	case !*i.extraInfo:
		return title
	case *i.fullExtraInfo:
		return title + i.renderedFullMeta
	default:
		return title + i.renderedMeta
	}
}

func (i *item) Description() string {
	return i.manga.Info().URL
}

func newItem(provider string, manga mangadata.Manga, info, fullInfo *bool) *item {
	i := &item{
		provider:      provider,
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
