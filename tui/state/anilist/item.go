package anilist

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	"github.com/luevano/libmangal/metadata"
)

var (
	_ list.Item        = (*item)(nil)
	_ list.DefaultItem = (*item)(nil)
)

// item implements list.item.
type item struct {
	meta metadata.Metadata
	// bad is a match too unlike the search to be used automatically, it can still be picked
	bad   bool
	score float64
}

// FilterValue implements list.Item.
func (i *item) FilterValue() string {
	title := i.meta.String()
	if title != "" {
		return title
	}
	return "Untitled"
}

// Item implements list.Item.
func (i *item) Title() string {
	return i.FilterValue()
}

// Description implements list.Item.
func (i *item) Description() string {
	url := fmt.Sprint("https://anilist.co/manga/", i.meta.ID().Value())
	if i.bad {
		return fmt.Sprintf("%s · bad match (%.0f%%)", url, i.score*100)
	}
	return url
}
