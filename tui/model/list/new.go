package list

import (
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/paginator"
	"charm.land/lipgloss/v2"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/tui/base"
)

func New[T any](
	itemHeight int,
	itemSpacing int,
	singular, plural string,
	items []T,
	transform func(T) list.DefaultItem,
) *Model {
	listItems := make([]list.Item, len(items))
	for i, item := range items {
		listItems[i] = transform(item)
	}

	border := lipgloss.ThickBorder()
	delegate := list.NewDefaultDelegate()

	// styles don't use mangal/theme/style, they're more specialized (paddings, borders)
	styles := itemStyles()
	styles.NormalTitle = styles.NormalTitle.Bold(true)
	styles.SelectedTitle = styles.SelectedTitle.Bold(true).
		Foreground(color.Accent).
		Border(border, false, false, false, true).
		BorderForeground(color.Accent)
	styles.SelectedDesc = styles.SelectedDesc.
		Foreground(styles.NormalDesc.GetForeground()).
		Border(border, false, false, false, true).
		BorderForeground(color.Accent)
	delegate.Styles = styles

	if itemHeight == 1 {
		delegate.ShowDescription = false
	}
	delegate.SetHeight(itemHeight)
	delegate.SetSpacing(itemSpacing)

	// If only a few items, just set the necessary hight, else the max (10 lines)
	perItemHeight := itemHeight + itemSpacing
	itemsHeight := perItemHeight * len(items)
	size := base.Size{Width: 20, Height: min(10, itemsHeight)}

	l := list.New(listItems, delegate, size.Width, size.Height)
	l.Styles = listStyles()
	// bubbles v2's list doesn't style its filter input
	l.FilterInput.SetStyles(l.Styles.Filter)
	l.FilterInput.Prompt = icon.Filter.Raw() + " "
	l.SetShowHelp(false)
	l.SetShowFilter(false)
	l.SetShowStatusBar(false)
	l.SetShowTitle(false)
	l.SetShowPagination(false)
	l.DisableQuitKeybindings()
	l.InfiniteScrolling = true

	l.Paginator.Type = paginator.Arabic

	l.SetStatusBarItemName(singular, plural)

	s := &Model{
		Model:    l,
		delegate: &delegate,
		size:     size,
	}
	// the help shows the keys the list has (and enables), not a copy's
	s.KeyMap = newKeyMap(&s.Model.KeyMap)
	// pages are h/l (and the arrows, pgup/pgdn) only: b u f d are screen keys
	s.Reserve("b", "u", "f", "d")
	s.updateKeybinds()
	return s
}

// itemStyles are bubbles' item styles with the colors of both backgrounds, picked when
// rendered (bubbles v2 takes the background when they're made, before the TUI knows it).
func itemStyles() list.DefaultItemStyles {
	l, d := list.NewDefaultItemStyles(false), list.NewDefaultItemStyles(true)
	return list.DefaultItemStyles{
		NormalTitle:   adapt(l.NormalTitle, d.NormalTitle),
		NormalDesc:    adapt(l.NormalDesc, d.NormalDesc),
		SelectedTitle: adapt(l.SelectedTitle, d.SelectedTitle),
		SelectedDesc:  adapt(l.SelectedDesc, d.SelectedDesc),
		DimmedTitle:   adapt(l.DimmedTitle, d.DimmedTitle),
		DimmedDesc:    adapt(l.DimmedDesc, d.DimmedDesc),
		FilterMatch:   d.FilterMatch,
	}
}

// listStyles are bubbles' list styles shown by mangal's lists (the rest are hidden)
// with the colors of both backgrounds.
func listStyles() list.Styles {
	l, d := list.DefaultStyles(false), list.DefaultStyles(true)
	s := d
	s.NoItems = adapt(l.NoItems, d.NoItems)
	s.Filter.Focused.Prompt = adapt(l.Filter.Focused.Prompt, d.Filter.Focused.Prompt)
	s.Filter.Blurred.Prompt = adapt(l.Filter.Blurred.Prompt, d.Filter.Blurred.Prompt)
	s.Filter.Blurred.Text = adapt(l.Filter.Blurred.Text, d.Filter.Blurred.Text)
	s.Filter.Cursor.Color = color.LightDark(l.Filter.Cursor.Color, d.Filter.Cursor.Color)
	return s
}

// adapt is dark with the foreground colors of light on a light background.
func adapt(light, dark lipgloss.Style) lipgloss.Style {
	s := dark
	if fl, fd := light.GetForeground(), dark.GetForeground(); fd != nil && fl != fd {
		s = s.Foreground(color.LightDark(fl, fd))
	}
	if bl, bd := light.GetBorderLeftForeground(), dark.GetBorderLeftForeground(); bd != nil && bl != bd {
		s = s.BorderForeground(color.LightDark(bl, bd))
	}
	return s
}
