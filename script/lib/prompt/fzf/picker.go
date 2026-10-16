package fzf

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/paginator"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/huhtheme"
	"github.com/luevano/mangal/v5/theme/icon"
)

// the picker's own keys: typing goes to the filter, so the TUI's k/j and h/l can't move
var (
	keyPick = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "pick"))
	// the form's quit key
	keyCancel   = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "cancel"))
	keyUp       = key.NewBinding(key.WithKeys("up", "ctrl+p"))
	keyDown     = key.NewBinding(key.WithKeys("down", "ctrl+n"))
	keyPrevPage = key.NewBinding(key.WithKeys("pgup"))
	keyNextPage = key.NewBinding(key.WithKeys("pgdown"))
)

type item string

func (i item) FilterValue() string { return string(i) }
func (i item) Title() string       { return string(i) }
func (i item) Description() string { return "" }

var _ huh.Field = (*picker)(nil)

// picker is a huh field: a full-screen list that is always filtering (list.DefaultFilter,
// sahilm/fuzzy: ranked, the matches highlighted). huh's Select only filters by substring
// (unexported, not pluggable) and keeps the options' order.
type picker struct {
	list   list.Model
	items  []string
	picked int // index in the items, -1 until one is picked
	height int
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

	return &picker{list: l, items: items, picked: -1}
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

func (p *picker) Init() tea.Cmd { return nil }

// Focus implements huh.Field: the form focuses it when it starts.
func (p *picker) Focus() tea.Cmd { return tea.RequestBackgroundColor }

func (p *picker) Update(msg tea.Msg) (huh.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.list.SetSize(msg.Width, msg.Height)
		// huh's group takes the height of its content as it was before this msg (and
		// caps it at the window's): once the list has the window's, a new size makes
		// the group as high
		if msg.Height != p.height {
			p.height = msg.Height
			return p, tea.RequestWindowSize
		}
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
			return p, huh.NextField
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

func (p *picker) View() string { return p.list.View() }

func (p *picker) Blur() tea.Cmd                            { return nil }
func (p *picker) Error() error                             { return nil }
func (p *picker) Run() error                               { return huh.Run(p) }
func (p *picker) Skip() bool                               { return false }
func (p *picker) Zoom() bool                               { return false }
func (p *picker) KeyBinds() []key.Binding                  { return []key.Binding{keyPick, keyCancel} }
func (p *picker) WithTheme(huh.Theme) huh.Field            { return p }
func (p *picker) WithKeyMap(*huh.KeyMap) huh.Field         { return p }
func (p *picker) WithWidth(int) huh.Field                  { return p }
func (p *picker) WithHeight(int) huh.Field                 { return p }
func (p *picker) WithPosition(huh.FieldPosition) huh.Field { return p }
func (p *picker) GetKey() string                           { return "" }
func (p *picker) GetValue() any                            { return p.picked }

// RunAccessible implements huh.Field (huh's accessible mode, e.g. TERM=dumb): the items
// numbered, a number picks one, an empty line cancels.
func (p *picker) RunAccessible(w io.Writer, r io.Reader) error {
	for i, it := range p.items {
		fmt.Fprintf(w, "%d. %s\n", i+1, it)
	}
	scanner := bufio.NewScanner(r)
	for {
		fmt.Fprintf(w, "Pick one (1-%d, empty to cancel): ", len(p.items))
		if !scanner.Scan() {
			fmt.Fprintln(w)
			return scanner.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			return nil
		}
		if n, err := strconv.Atoi(line); err == nil && n >= 1 && n <= len(p.items) {
			p.picked = n - 1
			return nil
		}
	}
}

// newForm is the form that runs the picker: full screen, esc and ctrl+c cancel.
func newForm(p *picker) *huh.Form {
	km := huh.NewDefaultKeyMap()
	km.Quit = keyCancel
	return huh.NewForm(huh.NewGroup(p)).
		WithShowHelp(false).
		WithShowErrors(false).
		WithKeyMap(km).
		WithTheme(huhtheme.New(nil)).
		WithViewHook(func(v tea.View) tea.View {
			v.AltScreen = true
			return v
		})
}

// pick shows the picker, ok is false when cancelled.
func pick(ctx context.Context, items []string) (index int, ok bool, err error) {
	if len(items) == 0 {
		return 0, false, nil
	}
	p := newPicker(items)
	// huh draws on stderr: a script's stdout may be piped
	err = newForm(p).RunWithContext(ctx)
	if ctx.Err() != nil {
		return 0, false, ctx.Err()
	}
	if err != nil && !errors.Is(err, huh.ErrUserAborted) {
		return 0, false, err
	}
	return p.picked, p.picked >= 0, nil
}
