package fzf

import (
	"context"
	"fmt"
	"io"
	"os"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/paginator"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/icon"
)

// the picker's own keys: typing goes to the filter, so the TUI's k/j and h/l can't move
var (
	keyPick     = key.NewBinding(key.WithKeys("enter"))
	keyCancel   = key.NewBinding(key.WithKeys("esc", "ctrl+c"))
	keyUp       = key.NewBinding(key.WithKeys("up", "ctrl+p"))
	keyDown     = key.NewBinding(key.WithKeys("down", "ctrl+n"))
	keyPrevPage = key.NewBinding(key.WithKeys("pgup"))
	keyNextPage = key.NewBinding(key.WithKeys("pgdown"))
)

type item string

func (i item) FilterValue() string { return string(i) }
func (i item) Title() string       { return string(i) }
func (i item) Description() string { return "" }

// picker is a full-screen list that is always filtering (list.DefaultFilter, sahilm/fuzzy).
type picker struct {
	list   list.Model
	picked int // index in the items, -1 until one is picked
}

func newPicker(items []string) *picker {
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = item(it)
	}

	l := list.New(listItems, newDelegate(true), 80, 20)
	l.FilterInput.SetStyles(l.Styles.Filter)
	l.FilterInput.Prompt = icon.Filter.Raw() + " "
	l.SetShowHelp(false)
	l.SetShowTitle(false)
	l.SetStatusBarItemName("item", "items")
	l.DisableQuitKeybindings()
	l.InfiniteScrolling = true
	l.Paginator.Type = paginator.Arabic
	l.SetFilterText("")
	l.SetFilterState(list.Filtering)

	return &picker{list: l, picked: -1}
}

// delegate draws the cursor while filtering, the default one only does after
type delegate struct{ styles list.DefaultItemStyles }

func newDelegate(isDark bool) delegate {
	// the TUI lists' style (tui/model/list)
	styles := list.NewDefaultItemStyles(isDark)
	styles.NormalTitle = styles.NormalTitle.Bold(true)
	styles.SelectedTitle = styles.SelectedTitle.Bold(true).
		Foreground(color.Accent).
		Border(lipgloss.ThickBorder(), false, false, false, true).
		BorderForeground(color.Accent)
	return delegate{styles: styles}
}

func (d delegate) Height() int                         { return 1 }
func (d delegate) Spacing() int                        { return 0 }
func (d delegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d delegate) Render(w io.Writer, m list.Model, index int, it list.Item) {
	style := d.styles.NormalTitle
	if index == m.Index() {
		style = d.styles.SelectedTitle
	}
	width := m.Width() - style.GetHorizontalFrameSize()
	title := ansi.Truncate(string(it.(item)), width, "…")
	if m.FilterValue() != "" {
		unmatched := style.Inline(true)
		title = lipgloss.StyleRunes(title, m.MatchesForItem(index), unmatched.Inherit(d.styles.FilterMatch), unmatched)
	}
	fmt.Fprint(w, style.Render(title))
}

func (p *picker) Init() tea.Cmd { return tea.RequestBackgroundColor }

func (p *picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.list.SetSize(msg.Width, msg.Height)
	case tea.BackgroundColorMsg:
		p.setDark(msg.IsDark())
	case tea.PasteMsg:
		p.input(msg)
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, keyPick):
			if p.list.SelectedItem() == nil {
				return p, nil
			}
			p.picked = p.list.GlobalIndex()
			// an empty filter shows every item without their indexes (bubbles' itemsAsFilterItems)
			if p.list.FilterInput.Value() == "" {
				p.picked = p.list.Index()
			}
			return p, tea.Quit
		case key.Matches(msg, keyCancel):
			return p, tea.Quit
		case key.Matches(msg, keyUp):
			p.list.CursorUp()
		case key.Matches(msg, keyDown):
			p.list.CursorDown()
		case key.Matches(msg, keyPrevPage):
			p.list.PrevPage()
		case key.Matches(msg, keyNextPage):
			p.list.NextPage()
		default:
			p.input(msg)
		}
	}
	return p, nil
}

// setDark styles the list for the terminal's background.
func (p *picker) setDark(isDark bool) {
	p.list.SetDelegate(newDelegate(isDark))
	p.list.Styles = list.DefaultStyles(isDark)
	p.list.FilterInput.SetStyles(p.list.Styles.Filter)
}

// input edits the filter (a key or a paste) and filters right away, not through the list's
// async command, so a slow filter can't land after a newer one
func (p *picker) input(msg tea.Msg) {
	input, _ := p.list.FilterInput.Update(msg)
	if input.Value() == p.list.FilterInput.Value() {
		p.list.FilterInput = input
		return
	}
	pos := input.Position()
	p.list.SetFilterText(input.Value())
	p.list.SetFilterState(list.Filtering)
	p.list.FilterInput.SetCursor(pos)
}

func (p *picker) View() tea.View {
	v := tea.NewView(p.list.View())
	v.AltScreen = true
	return v
}

// pick shows the picker, ok is false when cancelled.
func pick(ctx context.Context, items []string) (index int, ok bool, err error) {
	if len(items) == 0 {
		return 0, false, nil
	}
	p := newPicker(items)
	// stderr: a script's stdout may be piped
	_, err = tea.NewProgram(p, tea.WithContext(ctx), tea.WithOutput(os.Stderr)).Run()
	if ctx.Err() != nil {
		return 0, false, ctx.Err()
	}
	if err != nil {
		return 0, false, err
	}
	return p.picked, p.picked >= 0, nil
}
