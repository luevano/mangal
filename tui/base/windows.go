package base

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/util"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// windows are the open windows: the history, the shown one, the ones ahead.
func (m *model) windows() []State {
	all := slices.Clone(m.history.states)
	all = append(all, m.state)
	return append(all, m.forward...)
}

// current is the index of the shown window in windows.
func (m *model) current() int { return m.history.Size() }

// dropForward closes the windows ahead, it's how many.
func (m *model) dropForward() int {
	n := len(m.forward)
	for _, s := range m.forward {
		destroy(s)
	}
	m.forward = nil
	return n
}

// jump shows the window at index i of windows, the ones over it stay as windows ahead.
// It gets a RestoredMsg of request.
func (m *model) jump(i int, request State) tea.Cmd {
	all := m.windows()
	if i != m.current() {
		m.history.states = slices.Clone(all[:i])
		m.state = all[i]
		m.forward = slices.Clone(all[i+1:])
		log.Debug("tui: jump to %q", m.state.Title().Text)
	}
	return m.restore(request)
}

// closeAbove closes every window over the one at index i of windows, it's shown if one of
// them was.
func (m *model) closeAbove(i int) tea.Cmd {
	all := m.windows()
	cur := m.current()
	for _, s := range all[i+1:] {
		destroy(s)
	}
	if i >= cur {
		m.forward = slices.Clone(all[cur+1 : i+1])
		return nil
	}
	m.history.states = slices.Clone(all[:i])
	m.state = all[i]
	m.forward = nil
	log.Debug("tui: closed the windows over %q", m.state.Title().Text)
	return m.restore(nil)
}

// jumpMsg shows the open window with the key.
type jumpMsg struct{ key string }

// JumpTo shows the open window with the key (in the history or ahead), the ones over it
// stay; a notification says so when it isn't open.
func JumpTo(key string) tea.Cmd {
	return func() tea.Msg { return jumpMsg{key: key} }
}

func (m *model) jumpToKey(key string) tea.Cmd {
	if m.inViewport {
		m.hideViewport()
	}
	for i, s := range m.windows() {
		if key != "" && keyOf(s) == key {
			return m.jump(i, nil)
		}
	}
	return Notify("That screen isn't open anymore")
}

// switcher lists the windows to jump to one or close the ones over it.
type switcher struct {
	selected int
}

var switcherKeys = struct {
	up, down, jump, closeAbove, close key.Binding
}{
	up:         util.BindNamedKey("↑/k", "up", "up", "k", "left", "h"),
	down:       util.BindNamedKey("↓/j", "down", "down", "j", "right", "l"),
	jump:       util.Bind("go", "enter"),
	closeAbove: util.Bind("close the ones over it", "d"),
	close:      util.BindNamedKey("esc", "close", "esc", "ctrl+w"),
}

func (m *model) openSwitcher() {
	m.switcher = &switcher{selected: m.current()}
}

func (m *model) updateSwitcher(msg tea.KeyMsg) tea.Cmd {
	k, sw := switcherKeys, m.switcher
	last := len(m.windows()) - 1
	switch {
	case key.Matches(msg, m.keyMap.quit):
		m.switcher = nil
		return m.quit()
	case key.Matches(msg, k.up):
		sw.selected = max(0, sw.selected-1)
	case key.Matches(msg, k.down):
		sw.selected = min(last, sw.selected+1)
	case key.Matches(msg, k.jump):
		m.switcher = nil
		return m.jump(sw.selected, nil)
	case key.Matches(msg, k.closeAbove):
		if sw.selected == last {
			return Notify("Nothing over it")
		}
		n := last - sw.selected
		cmd := m.closeAbove(sw.selected)
		return tea.Batch(cmd, Notify("Closed "+stringutil.Quantify(n, "window", "windows")))
	case key.Matches(msg, k.close):
		m.switcher = nil
	}
	return nil
}

func (m *model) switcherKeyMap() help.KeyMap {
	k := switcherKeys
	return util.KeyMap{Short: []key.Binding{k.up, k.down, k.jump, k.closeAbove, k.close}}
}

func (m *model) viewSwitcher(behind string) string {
	all := m.windows()
	cur, sel := m.current(), m.switcher.selected
	// the lines around the selected one that fit
	height := max(3, m.stateSize().Height-4)
	from := max(0, min(sel-height/2, len(all)-height))
	to := min(len(all), from+height)

	var b strings.Builder
	b.WriteString(style.Bold.Accent.Render("Windows"))
	for i := from; i < to; i++ {
		line := strings.Repeat("  ", i) + all[i].Title().Text
		switch {
		case i == cur:
			line += " (shown)"
		case i > cur:
			line = style.Normal.Secondary.Render(line + " (ahead)")
		}
		prefix := "  "
		if i == sel {
			prefix = style.Bold.Accent.Render("> ")
		}
		b.WriteString("\n" + prefix + line)
	}
	return m.dialog(b.String(), color.Accent, behind)
}
