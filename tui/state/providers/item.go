package providers

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/zyedidia/generic/set"
)

var (
	_ list.Item        = (*item)(nil)
	_ list.DefaultItem = (*item)(nil)
)

// item implements list.item.
type item struct {
	loader      libmangal.ProviderLoader
	loadedItems *set.Set[*item]
	loadTime    time.Time
	extraInfo   *bool
}

// FilterValue implements list.Item.
func (i *item) FilterValue() string {
	return i.loader.String()
}

// Title implements list.DefaultItem.
func (i *item) Title() string {
	var title strings.Builder
	title.WriteString(i.FilterValue())

	if i.isLoaded() {
		title.WriteString(" ")
		title.WriteString(icon.Check.Colored())
		if *i.extraInfo {
			timeAgo := fmt.Sprintf(" loaded %s ago", time.Since(i.loadTime).Truncate(time.Second).String())
			title.WriteString(style.Normal.Secondary.Render(timeAgo))
		}
	}

	return title.String()
}

// Description implements list.DefaultItem.
func (i *item) Description() string {
	info := i.loader.Info()
	website := info.Website
	if len(info.History) != 0 {
		website += style.Normal.Secondary.Render(" (formerly " + formerly(info) + ")")
	}
	if *i.extraInfo {
		moreInfo := fmt.Sprintf("%s v%s", info.ID, info.Version)
		return lipgloss.JoinVertical(lipgloss.Left, moreInfo, website)
	}

	return website
}

// formerly lists the previous names (or ids) of the provider, newest first.
func formerly(info libmangal.ProviderInfo) string {
	var names []string
	for _, alias := range info.History {
		name := alias.Name
		if name == "" || name == info.Name {
			name = alias.ID
		}
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return strings.Join(names, ", ")
}

func (i *item) isLoaded() bool {
	return i.loadedItems.Has(i)
}

func (i *item) markLoaded() {
	if !i.isLoaded() {
		i.loadTime = time.Now()
		i.loadedItems.Put(i)
	}
}

func (i *item) markClosed() {
	if i.isLoaded() {
		i.loadedItems.Remove(i)
	}
}
