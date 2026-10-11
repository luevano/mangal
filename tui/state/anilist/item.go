package anilist

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/storage"
)

var (
	_ list.Item        = (*item)(nil)
	_ list.DefaultItem = (*item)(nil)
	_ list.DefaultItem = (*sourceItem)(nil)
)

// item is a search result.
type item struct {
	src  client.MetadataSource
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
	url := i.src.URL + i.meta.ID().Raw
	if i.bad {
		return fmt.Sprintf("%s · bad match (%.0f%%)", url, i.score*100)
	}
	return url
}

// sourceItem is a metadata source of the bindings screen with the manga's binding.
type sourceItem struct {
	src client.MetadataSource
	// binding is nil when it isn't bound
	binding *storage.Binding
	// meta is the bound manga's metadata, nil until loaded (or if it failed, then err)
	meta metadata.Metadata
	err  error
	// matched is the manga's metadata from this source when it isn't bound (searched)
	matched metadata.Metadata
}

// FilterValue implements list.Item.
func (i *sourceItem) FilterValue() string { return i.src.Name }

// Title implements list.Item.
func (i *sourceItem) Title() string {
	if i.binding != nil && i.binding.Default {
		return i.src.Name + " (default)"
	}
	return i.src.Name
}

// Description implements list.Item.
func (i *sourceItem) Description() string {
	switch {
	case i.binding == nil && i.matched != nil:
		return fmt.Sprintf("not bound · searched match: %s", i.matched)
	case i.binding == nil:
		return "not bound"
	case i.err != nil:
		return fmt.Sprintf("%s · %s", i.binding.ID, i.err)
	case i.meta == nil:
		return i.binding.ID
	}
	return fmt.Sprintf("%s · %s", i.meta, i.src.URL+i.binding.ID)
}
