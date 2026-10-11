package list

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/paginator"
	"github.com/charmbracelet/lipgloss"
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

	// TODO: possibly use the current "window" (where the list is being displayed) accent color,
	// instead of always hardcoding color.Accent
	//
	// Styles don't use mangal/theme/style, as they are more specialized with paddings and whatnot
	styles := delegate.Styles
	styles.NormalTitle = styles.NormalTitle.Bold(true)
	styles.SelectedTitle = styles.SelectedTitle.Bold(true).
		Foreground(color.Accent).
		Border(border, false, false, false, true).
		BorderForeground(color.Accent)
	styles.SelectedDesc = styles.SelectedDesc.
		Foreground(delegate.Styles.NormalDesc.GetForeground()).
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
	s.updateKeybinds()
	return s
}
