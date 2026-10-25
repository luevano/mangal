// Package config is the TUI's config window: every config key, changed in place.
package config

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	_list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	mconfig "github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	"github.com/luevano/mangal/v5/tui/model/huhform"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/util"
)

var _ base.ConfigWindow = (*state)(nil)

type mode int

const (
	browsing mode = iota
	// a text input for the selected key
	editing
	// the entries of a map key
	mapping
)

// detailsHeight is the lines under the list: the selected key's description, hint and
// input or error.
const detailsHeight = 7

type state struct {
	// list has the section headers and the keys of the expanded ones, or every key while
	// searching
	list *list.Model
	// expanded sections, searching is a search through every key (collapsed ones too)
	expanded  map[string]bool
	searching bool
	mode      mode
	// key being edited or mapped
	key   string
	input *huhform.Field
	// form has the input
	form *huhform.Form
	// err is the last change's error, shown inline
	err  string
	maps mapEditor
	size base.Size
	// confirmReset is the map key whose reset (removing every entry) waits for a second
	// press of the reset key
	confirmReset string
	// Host asks before an entry of a map is removed
	confirm.Host

	keys keyMap

	// write saves the change to the config file, reload marks the loaded providers stale
	write  func(key string, raw any) error
	reload func()
}

// mapEditor edits the entries of a map key.
type mapEditor struct {
	ids    []string
	cursor int
	// inputting the url of id, or the id of a new entry when adding
	inputting, adding bool
	id                string
}

// New is the config window, filtered to filter if not empty.
func New(filter string) base.State {
	keys := mconfig.Keys()
	l := list.New(2, 0, "key", "keys", sortedItems(keys), func(i item) _list.DefaultItem { return i })
	input := huhform.Text("", "", "", nil)
	s := &state{
		list:     l,
		expanded: map[string]bool{},
		input:    input,
		form:     huhform.New(input),
		keys:     newKeyMap(),
		Host:     confirm.NewHost(40, color.Warning),
		write:    writeKey,
		reload:   base.ReloadProviders,
	}
	s.showRows()
	s.Filter(filter)
	return s
}

// rows are the section headers with the keys of the expanded ones, or every key while
// searching.
func (s *state) rows() []_list.Item {
	var rows []_list.Item
	for _, section := range sectionItems(mconfig.Keys()) {
		if !s.searching {
			section.expanded = s.expanded[section.name]
			rows = append(rows, section)
			if !section.expanded {
				continue
			}
		}
		for _, key := range section.keys {
			i := item{key: key}
			if !s.searching && key != section.name {
				i.prefix = section.name + "."
			}
			rows = append(rows, i)
		}
	}
	return rows
}

// rowID is what a row is (a key, or a section header), to keep it selected.
func rowID(row _list.Item) string {
	switch r := row.(type) {
	case item:
		return r.key
	case sectionItem:
		return "section:" + r.name
	}
	return ""
}

// showRows lists the rows again, the selected one kept (or its section's header).
func (s *state) showRows() tea.Cmd {
	selected := rowID(s.list.SelectedItem())
	cmd := s.list.SetItems(s.rows())
	// SetItems enables it again
	s.list.KeyMap.Reverse.SetEnabled(false)
	section, _, _ := strings.Cut(strings.TrimPrefix(selected, "section:"), ".")
	for n, row := range s.list.Items() {
		if id := rowID(row); id == selected || (id == "section:"+section && !s.expanded[section]) {
			s.list.Select(n)
			break
		}
	}
	return cmd
}

// search lists every key for a search through all of them.
func (s *state) search() tea.Cmd {
	if s.searching {
		return nil
	}
	s.searching = true
	return s.showRows()
}

// toggleAll expands every section, or collapses them all when they all are.
func (s *state) toggleAll() tea.Cmd {
	sections := sectionItems(mconfig.Keys())
	all := true
	for _, section := range sections {
		all = all && s.expanded[section.name]
	}
	for _, section := range sections {
		s.expanded[section.name] = !all
	}
	return s.showRows()
}

func writeKey(key string, raw any) error {
	if err := mconfig.CreateFile(); err != nil {
		return err
	}
	return mconfig.WriteKey(key, raw)
}

// Filter implements base.ConfigWindow: a search through every key.
func (s *state) Filter(filter string) tea.Cmd {
	s.mode = browsing
	s.err = ""
	if filter == "" {
		s.list.ResetFilter()
		return nil
	}
	s.search()
	s.list.SetFilterText(filter)
	// the key itself first, the fuzzy matches after it
	for n, i := range s.list.VisibleItems() {
		if i.(item).key == filter {
			s.list.Select(n)
		}
	}
	return nil
}

func (s *state) Key() string       { return "config" }
func (s *state) Title() base.Title { return base.Title{Text: "Config"} }

func (s *state) Subtitle() string {
	if s.searching {
		return s.list.Subtitle() + ", every section"
	}
	return fmt.Sprintf("%d keys in %s", len(mconfig.Keys()), mconfig.Path)
}

func (s *state) Status() string { return s.list.Status() }

func (s *state) Backable() bool {
	return s.Host.Backable(s.mode == browsing && s.list.Unfiltered())
}

func (s *state) Inputting() bool {
	return s.Host.Inputting(s.list.Filtering() || s.mode == editing || (s.mode == mapping && s.maps.inputting))
}

func (s *state) KeyMap() help.KeyMap { return s.Host.KeyMap(s.keyMap()) }

func (s *state) keyMap() help.KeyMap {
	switch s.mode {
	case editing:
		return util.KeyMap{Short: []key.Binding{s.keys.save, s.keys.cancel}}
	case mapping:
		if s.maps.inputting {
			return util.KeyMap{Short: []key.Binding{s.keys.save, s.keys.cancel}}
		}
		return util.KeyMap{Short: []key.Binding{s.keys.add, s.keys.editEntry, s.keys.remove, s.keys.up, s.keys.down, s.keys.close}}
	}
	return base.CombinedKeyMap(util.KeyMap{Short: []key.Binding{s.keys.change, s.keys.reset, s.keys.expandAll}}, s.list.KeyMap)
}

func (s *state) Resize(size base.Size) tea.Cmd {
	s.size = size
	s.input.SetWidth(max(10, size.Width-4))
	return s.Host.Resize(size, func(size base.Size) tea.Cmd {
		return s.list.Resize(base.Size{Width: size.Width, Height: max(2, size.Height-detailsHeight)})
	})
}

func (s *state) Init(context.Context) tea.Cmd { return nil }

func (s *state) selected() (string, bool) {
	i, ok := s.list.SelectedItem().(item)
	return i.key, ok
}

// removeEntryMsg is the removal of a map entry, confirmed.
type removeEntryMsg struct{ key, id string }

func (s *state) Update(_ context.Context, msg tea.Msg) tea.Cmd {
	if cmd, ok := s.Answer(msg); ok {
		return cmd
	}
	if msg, ok := msg.(removeEntryMsg); ok {
		return s.removeEntry(msg)
	}
	switch s.mode {
	case editing:
		return s.updateEditing(msg)
	case mapping:
		return s.updateMapping(msg)
	}
	if msg, ok := msg.(tea.KeyPressMsg); ok && !s.list.Filtering() {
		if section, ok := s.list.SelectedItem().(sectionItem); ok && key.Matches(msg, s.keys.change) {
			s.expanded[section.name] = !s.expanded[section.name]
			return s.showRows()
		}
		switch {
		case key.Matches(msg, s.keys.expandAll) && !s.searching:
			return s.toggleAll()
		case key.Matches(msg, s.list.KeyMap.List.Filter) && s.list.Unfiltered():
			// the search goes through every key, the collapsed sections' too
			return tea.Batch(s.search(), s.list.Update(msg))
		}
		k, ok := s.selected()
		confirmed := s.confirmReset == k
		s.confirmReset = ""
		switch {
		case !ok:
		case key.Matches(msg, s.keys.change):
			return s.change(k)
		case key.Matches(msg, s.keys.reset):
			if mconfig.KindOf(k) == mconfig.KindMap && !confirmed {
				s.confirmReset = k
				return base.Notify("Press " + s.keys.reset.Help().Key + " again to remove every entry")
			}
			def, _ := mconfig.Default(k)
			cmd, _ := s.set(k, def)
			return cmd
		}
	}
	cmd := s.list.Update(msg)
	if s.searching && s.list.Unfiltered() && !s.list.Filtering() {
		// the search ended (esc): back to the sections as they were
		s.searching = false
		return tea.Batch(cmd, s.showRows())
	}
	return cmd
}

// change toggles a bool, cycles an enum, or opens the editor of the key.
func (s *state) change(key string) tea.Cmd {
	s.err = ""
	get := mconfig.Get
	if _, set := mconfig.EnvVar(key); set {
		// the change goes to the file, start from what's there, not the override
		get = mconfig.FileValue
	}
	raw, err := get(key)
	if err != nil {
		def, _ := mconfig.Default(key)
		raw = def
	}
	switch mconfig.KindOf(key) {
	case mconfig.KindBool:
		b, _ := raw.(bool)
		cmd, _ := s.set(key, !b)
		return cmd
	case mconfig.KindMap:
		m, _ := raw.(map[string]any)
		s.mode = mapping
		s.key = key
		s.maps = mapEditor{ids: slices.Sorted(maps.Keys(m))}
		return nil
	}
	if values := mconfig.Values(key); values != nil {
		current := fmt.Sprint(raw)
		next := values[0]
		for n, v := range values {
			if strings.EqualFold(v, current) {
				next = values[(n+1)%len(values)]
			}
		}
		cmd, _ := s.set(key, next)
		return cmd
	}
	s.mode = editing
	s.key = key
	return s.openInput(fmt.Sprint(raw), "", mconfig.IsSecret(key))
}

func (s *state) openInput(value, placeholder string, secret bool) tea.Cmd {
	s.input.SetValue(value)
	s.input.SetPlaceholder(placeholder)
	s.input.SetSecret(secret)
	return s.form.Focus(0)
}

// set validates the value, sets it for this run and writes it to the config file. ok is
// false for an invalid value, nothing changes then.
func (s *state) set(key string, value any) (cmd tea.Cmd, ok bool) {
	raw, err := mconfig.Parse(key, value)
	if err != nil {
		s.err = errorText(key, err)
		return nil, false
	}
	// in Update: viper isn't safe for concurrent use, and the screens read it to render
	if err := mconfig.Set(key, raw); err != nil {
		s.err = errorText(key, err)
		return nil, false
	}
	s.err = ""
	notice, warn := key+" saved", false
	if err := s.write(key, raw); err != nil {
		s.err = "set for this run but not saved: " + err.Error()
		notice = key + " set"
	} else if mode, readable := mconfig.ReadableByOthers(); readable && mconfig.IsSecret(key) && mconfig.StoredIn(key) == mconfig.StoredFile {
		notice = fmt.Sprintf("%s saved, but others can read the config file (mode %o): chmod 600 it", key, mode)
		warn = true
	}
	switch effectOf(key) {
	case applyReload:
		s.reload()
	case applyIcons:
		icon.SetType(mconfig.Icons.Get())
	}
	if warn {
		return base.NotifyWarning(notice), true
	}
	return base.Notify(notice), true
}

// errorText is the error without the key, shown next to it.
func errorText(key string, err error) string {
	return strings.TrimPrefix(err.Error(), fmt.Sprintf("config key %q: ", key))
}

func (s *state) updateEditing(msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(msg, s.keys.save):
			cmd, ok := s.set(s.key, s.input.Raw())
			if ok {
				s.closeInput()
			}
			return cmd
		case key.Matches(msg, s.keys.cancel):
			s.closeInput()
			s.err = ""
			return nil
		}
	}
	_, cmd := s.form.Update(msg)
	return cmd
}

func (s *state) closeInput() {
	s.input.SetValue("")
	if s.mode == editing {
		s.mode = browsing
	}
	s.maps.inputting = false
}

func (s *state) updateMapping(msg tea.Msg) tea.Cmd {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if s.maps.inputting {
		if ok && key.Matches(keyMsg, s.keys.save) {
			return s.saveEntry()
		}
		if ok && key.Matches(keyMsg, s.keys.cancel) {
			s.closeInput()
			s.err = ""
			return nil
		}
		_, cmd := s.form.Update(msg)
		return cmd
	}
	if !ok {
		return nil
	}
	m := &s.maps
	switch {
	case key.Matches(keyMsg, s.keys.close):
		s.mode = browsing
		s.err = ""
	case key.Matches(keyMsg, s.keys.up):
		if len(m.ids) > 0 {
			m.cursor = (m.cursor - 1 + len(m.ids)) % len(m.ids)
		}
	case key.Matches(keyMsg, s.keys.down):
		if len(m.ids) > 0 {
			m.cursor = (m.cursor + 1) % len(m.ids)
		}
	case key.Matches(keyMsg, s.keys.add):
		m.inputting, m.adding, m.id = true, true, ""
		s.err = ""
		return s.openInput("", "provider id", false)
	case key.Matches(keyMsg, s.keys.editEntry):
		if len(m.ids) == 0 {
			return nil
		}
		m.inputting, m.adding, m.id = true, false, m.ids[m.cursor]
		s.err = ""
		return s.openInput(s.entries()[m.id].(string), "url", false)
	case key.Matches(keyMsg, s.keys.remove):
		if len(m.ids) == 0 {
			return nil
		}
		k, id := s.key, m.ids[m.cursor]
		s.Confirm.AskDelete("Remove", fmt.Sprintf("Remove %s from %s?", id, k), s.View(), func() tea.Msg { return removeEntryMsg{key: k, id: id} })
	}
	return nil
}

func (s *state) removeEntry(msg removeEntryMsg) tea.Cmd {
	if s.mode != mapping || s.key != msg.key {
		return nil
	}
	entries := s.entries()
	delete(entries, msg.id)
	cmd, ok := s.set(s.key, entries)
	if ok {
		s.syncIDs()
	}
	return cmd
}

// saveEntry takes the id of a new entry, or sets the url of the entry.
func (s *state) saveEntry() tea.Cmd {
	m := &s.maps
	value := s.input.Value()
	if m.adding && m.id == "" {
		if value == "" {
			s.err = "the id is empty"
			return nil
		}
		m.id = value
		url := ""
		if v, ok := s.entries()[value].(string); ok {
			url = v
		}
		return s.openInput(url, "url", false)
	}
	entries := s.entries()
	entries[m.id] = value
	cmd, ok := s.set(s.key, entries)
	if ok {
		id := m.id
		s.closeInput()
		s.syncIDs()
		m.cursor = max(0, slices.Index(m.ids, id))
	}
	return cmd
}

// entries is a copy of the map key's entries.
func (s *state) entries() map[string]any {
	raw, _ := mconfig.Get(s.key)
	m, _ := raw.(map[string]any)
	return maps.Clone(m)
}

func (s *state) syncIDs() {
	s.maps.ids = slices.Sorted(maps.Keys(s.entries()))
	s.maps.cursor = min(s.maps.cursor, max(0, len(s.maps.ids)-1))
}

func (s *state) View() string {
	return s.Host.View(func() string {
		if s.mode == mapping {
			return s.viewMap()
		}
		return lipgloss.JoinVertical(lipgloss.Left, s.list.View(), s.viewDetails())
	})
}

func (s *state) viewDetails() string {
	key, ok := s.selected()
	if s.mode == editing {
		key, ok = s.key, true
	}
	if !ok {
		return ""
	}
	width := max(20, s.size.Width)
	lines := []string{style.Bold.Accent.Render(key)}
	description, _ := mconfig.Description(key)
	desc := lipgloss.NewStyle().Width(width).Render(description)
	lines = append(lines, strings.Split(desc, "\n")...)
	if len(lines) > 4 {
		lines = append(lines[:3], lines[3]+"…")
	}

	var extra []string
	if !mconfig.IsSecret(key) && mconfig.KindOf(key) != mconfig.KindMap {
		if def, _ := mconfig.Default(key); def != nil {
			extra = append(extra, fmt.Sprintf("default %v", def))
		}
	}
	if hint := effectOf(key).hint(); hint != "" {
		extra = append(extra, hint)
	}
	if name, set := mconfig.EnvVar(key); set {
		extra = append(extra, style.Normal.Warning.Render(name+" overrides the file"))
	}
	if len(extra) > 0 {
		lines = append(lines, style.Normal.Secondary.Width(width).Render(strings.Join(extra, " · ")))
	}
	if s.mode == editing {
		lines = append(lines, s.form.View())
	}
	if s.err != "" {
		lines = append(lines, style.Normal.Error.Width(width).Render(s.err))
	}
	return lipgloss.NewStyle().Height(detailsHeight).MaxHeight(detailsHeight).Render(strings.Join(lines, "\n"))
}

func (s *state) viewMap() string {
	m := s.maps
	lines := []string{style.Bold.Accent.Render(s.key), ""}
	entries := s.entries()
	if len(m.ids) == 0 {
		lines = append(lines, style.Normal.Secondary.Render("no entries, "+s.keys.add.Help().Key+" adds one"))
	}
	for n, id := range m.ids {
		cursor := "  "
		line := fmt.Sprintf("%s = %v", id, entries[id])
		if n == m.cursor {
			cursor = style.Bold.Accent.Render(icon.Item.Raw()) + " "
			line = style.Bold.Base.Render(line)
		}
		lines = append(lines, cursor+line)
	}
	if m.inputting {
		label := "url of " + m.id
		if m.adding && m.id == "" {
			label = "new entry: provider id"
		}
		lines = append(lines, "", style.Normal.Secondary.Render(label), s.form.View())
	}
	if s.err != "" {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(color.Error).Width(max(20, s.size.Width)).Render(s.err))
	}
	return strings.Join(lines, "\n")
}
