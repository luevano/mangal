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
	"github.com/charmbracelet/x/ansi"
	mconfig "github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/storage"
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
	// the settings' history
	historying
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
	// cfg has the settings' history and its undo, the services' Config when nil
	cfg     core.Config
	history historyView

	// remote is the server of a remote TUI: its keys (server, by key) are shown and set
	// there, the local-side ones (config.Side) stay this machine's; filter is the
	// window's, again once they're loaded
	remote *base.Remote
	server map[string]core.ConfigEntry
	filter string
}

// serverListMsg are the server's keys, or why not.
type serverListMsg struct {
	entries []core.ConfigEntry
	err     error
}

// serverMsg is a key of the server as a change left it, or why it wasn't changed.
type serverMsg struct {
	key string
	e   core.ConfigEntry
	err error
}

// historyView is the settings' history: every key's, or one's.
type historyView struct {
	key     string
	changes []core.ConfigChange
	cursor  int
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
	keys := mconfig.ListedKeys()
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
		remote:   base.CurrentRemote,
		filter:   filter,
	}
	if s.remote != nil {
		s.server = map[string]core.ConfigEntry{}
	}
	s.showRows()
	s.Filter(filter)
	return s
}

// rows are the section headers with the keys of the expanded ones, or every key while
// searching.
func (s *state) rows() []_list.Item {
	var rows []_list.Item
	for _, section := range sectionsOf(s.items()) {
		if !s.searching {
			section.expanded = s.expanded[section.name]
			rows = append(rows, section)
			if !section.expanded {
				continue
			}
		}
		for _, i := range section.items {
			if !s.searching && i.key != section.name {
				i.prefix = section.name + "."
			}
			rows = append(rows, i)
		}
	}
	return rows
}

// items are the keys by section: on a remote this machine's local-side ones and the
// server's others.
func (s *state) items() []item {
	keys := mconfig.ListedKeys()
	if s.remote == nil {
		return sortedItems(keys)
	}
	var items []item
	for _, key := range keys {
		if mconfig.Side(key) == "local" {
			items = append(items, item{key: key})
		}
	}
	for _, e := range s.server {
		if e.Side != "local" {
			items = append(items, item{key: e.Key, e: &e})
		}
	}
	return sortItems(items)
}

// itemOf is the key's item, the server's on a remote.
func (s *state) itemOf(key string) item {
	if e, ok := s.server[key]; ok && e.Side != "local" {
		return item{key: key, e: &e}
	}
	return item{key: key}
}

// loadServer reads the server's keys, on a remote.
func (s *state) loadServer(ctx context.Context) tea.Cmd {
	if s.remote == nil {
		return nil
	}
	cfg, err := s.config()
	if err != nil {
		s.err = err.Error()
		return nil
	}
	return base.Load(ctx, "Reading the server's settings", func(ctx context.Context) (serverListMsg, error) {
		list, err := cfg.List(ctx)
		return serverListMsg{entries: list, err: err}, nil
	}, func(m serverListMsg) tea.Msg { return m })
}

// serverRefusal is why the server's key can't be changed from here, "" when it can (or
// the server says: serve.hide_unavailable false).
func (s *state) serverRefusal(i item) string {
	switch {
	case i.secret():
		return "a secret isn't set over HTTP: mangal config set on the server"
	case !s.remote.Has(core.ScopeAdmin) && !s.remote.ShowAll:
		return "changing the server's settings needs the admin permission on " + s.remote.Host()
	}
	return ""
}

// setServer sets the server's key, its serverMsg says how it went.
func (s *state) setServer(ctx context.Context, key string, value any) tea.Cmd {
	cfg, err := s.config()
	if err != nil {
		s.err = err.Error()
		return nil
	}
	return base.Load(ctx, "Saving "+key+" on the server", func(ctx context.Context) (serverMsg, error) {
		e, err := cfg.Set(ctx, key, value)
		return serverMsg{key: key, e: e, err: err}, nil
	}, func(m serverMsg) tea.Msg { return m })
}

// serverAnswer applies the server's answer to a change.
func (s *state) serverAnswer(msg serverMsg) tea.Cmd {
	if msg.err != nil {
		s.err = errorText(msg.key, msg.err)
		return nil
	}
	s.err = ""
	s.server[msg.key] = msg.e
	if s.mode == mapping && s.key == msg.key {
		s.syncIDs()
	}
	cmd := s.showRows()
	if msg.e.Applies == "restart" {
		return tea.Batch(cmd, base.NotifyWarning(msg.key+" saved on the server: mangal serve uses it once restarted"))
	}
	return tea.Batch(cmd, base.Notify(msg.key+" saved on the server"))
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
	section, isSection := strings.CutPrefix(selected, "section:")
	if !isSection {
		section = sectionOf(selected)
	}
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
	sections := sectionsOf(s.items())
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
	if s.remote != nil {
		return fmt.Sprintf("%d keys: the server's (%s), this machine's tui, icons, cli and remote", len(s.items()), s.remote.Host())
	}
	return fmt.Sprintf("%d keys: %s and the database", len(mconfig.ListedKeys()), mconfig.Path)
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
	case historying:
		return util.KeyMap{Short: []key.Binding{s.keys.undo, s.keys.up, s.keys.down, s.keys.close}}
	}
	return base.CombinedKeyMap(util.KeyMap{Short: []key.Binding{s.keys.change, s.keys.reset, s.keys.expandAll, s.keys.history}}, s.list.KeyMap)
}

func (s *state) Resize(size base.Size) tea.Cmd {
	s.size = size
	s.input.SetWidth(max(10, size.Width-4))
	return s.Host.Resize(size, func(size base.Size) tea.Cmd {
		return s.list.Resize(base.Size{Width: size.Width, Height: max(2, size.Height-detailsHeight)})
	})
}

func (s *state) Init(ctx context.Context) tea.Cmd { return s.loadServer(ctx) }

func (s *state) selected() (string, bool) {
	i, ok := s.list.SelectedItem().(item)
	return i.key, ok
}

// removeEntryMsg is the removal of a map entry, confirmed.
type removeEntryMsg struct{ key, id string }

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if cmd, ok := s.Answer(msg); ok {
		return cmd
	}
	switch msg := msg.(type) {
	case removeEntryMsg:
		return s.removeEntry(ctx, msg)
	case serverListMsg:
		if msg.err != nil {
			s.err = "the server's settings: " + msg.err.Error()
			return nil
		}
		for _, e := range msg.entries {
			s.server[e.Key] = e
		}
		cmd := s.showRows()
		if k, _ := s.selected(); s.filter != "" && k != s.filter && s.mode == browsing {
			cmd = tea.Batch(cmd, s.Filter(s.filter))
		}
		s.filter = ""
		return cmd
	case serverMsg:
		return s.serverAnswer(msg)
	case base.DBChangedMsg:
		if s.remote != nil && msg.Has(storage.RevSettings) {
			return s.loadServer(ctx)
		}
	}
	switch s.mode {
	case editing:
		return s.updateEditing(ctx, msg)
	case mapping:
		return s.updateMapping(ctx, msg)
	case historying:
		return s.updateHistory(ctx, msg)
	}
	if msg, ok := msg.(tea.KeyPressMsg); ok && !s.list.Filtering() {
		if key.Matches(msg, s.keys.history) {
			k, _ := s.selected()
			return s.openHistory(ctx, k)
		}
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
			return s.change(ctx, k)
		case key.Matches(msg, s.keys.reset):
			i := s.itemOf(k)
			if i.kind() == mconfig.KindMap && !confirmed {
				s.confirmReset = k
				return base.Notify("Press " + s.keys.reset.Help().Key + " again to remove every entry")
			}
			cmd, _ := s.set(ctx, k, i.def())
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
func (s *state) change(ctx context.Context, key string) tea.Cmd {
	s.err = ""
	i := s.itemOf(key)
	get := mconfig.Get
	if _, set := mconfig.EnvVar(key); set {
		// the change goes to the file, start from what's there, not the override
		get = mconfig.FileValue
	}
	if i.e != nil {
		if s.err = s.serverRefusal(i); s.err != "" {
			return nil
		}
		get = func(string) (any, error) { return i.raw() }
	}
	raw, err := get(key)
	if err != nil {
		raw = i.def()
	}
	switch i.kind() {
	case mconfig.KindBool:
		b, _ := raw.(bool)
		cmd, _ := s.set(ctx, key, !b)
		return cmd
	case mconfig.KindMap:
		m, _ := raw.(map[string]any)
		s.mode = mapping
		s.key = key
		s.maps = mapEditor{ids: slices.Sorted(maps.Keys(m))}
		return nil
	}
	if values := i.values(); values != nil {
		current := fmt.Sprint(raw)
		next := values[0]
		for n, v := range values {
			if strings.EqualFold(v, current) {
				next = values[(n+1)%len(values)]
			}
		}
		cmd, _ := s.set(ctx, key, next)
		return cmd
	}
	s.mode = editing
	s.key = key
	return s.openInput(fmt.Sprint(raw), "", i.secret())
}

func (s *state) openInput(value, placeholder string, secret bool) tea.Cmd {
	s.input.SetValue(value)
	s.input.SetPlaceholder(placeholder)
	s.input.SetSecret(secret)
	return s.form.Focus(0)
}

// set validates the value, sets it for this run and writes it to the config file. ok is
// false for an invalid value, nothing changes then. A server's key is sent to the server:
// its serverMsg says how it went.
func (s *state) set(ctx context.Context, key string, value any) (cmd tea.Cmd, ok bool) {
	if i := s.itemOf(key); i.e != nil {
		if s.err = s.serverRefusal(i); s.err != "" {
			return nil, false
		}
		return s.setServer(ctx, key, value), true
	}
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
	if mconfig.Applies(key) == "restart" && s.err == "" {
		notice, warn = key+" saved: mangal reads it when it starts, restart mangal (and a running mangal serve) to use it", true
	}
	if warn {
		return base.NotifyWarning(notice), true
	}
	return base.Notify(notice), true
}

func (s *state) config() (core.Config, error) {
	if s.cfg != nil {
		return s.cfg, nil
	}
	svc, err := base.Services()
	return svc.Config, err
}

// openHistory lists the changes of key, every key's when "".
func (s *state) openHistory(ctx context.Context, key string) tea.Cmd {
	cfg, err := s.config()
	var changes []core.ConfigChange
	if err == nil {
		changes, err = cfg.History(ctx, key, 200)
	}
	if err != nil {
		s.err = err.Error()
		return nil
	}
	s.err = ""
	s.mode = historying
	s.history = historyView{key: key, changes: changes, cursor: min(s.history.cursor, max(0, len(changes)-1))}
	return nil
}

func (s *state) updateHistory(ctx context.Context, msg tea.Msg) tea.Cmd {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	h := &s.history
	switch {
	case key.Matches(keyMsg, s.keys.close):
		s.mode = browsing
		s.err = ""
	case key.Matches(keyMsg, s.keys.up):
		if len(h.changes) > 0 {
			h.cursor = (h.cursor - 1 + len(h.changes)) % len(h.changes)
		}
	case key.Matches(keyMsg, s.keys.down):
		if len(h.changes) > 0 {
			h.cursor = (h.cursor + 1) % len(h.changes)
		}
	case key.Matches(keyMsg, s.keys.undo):
		if len(h.changes) == 0 {
			return nil
		}
		c := h.changes[h.cursor]
		cfg, err := s.config()
		var e core.ConfigEntry
		if err == nil {
			e, err = cfg.Undo(ctx, c.ID)
		}
		if err != nil {
			s.err = err.Error()
			return nil
		}
		// a value set earlier in this run would hide the stored one
		if _, ok := s.server[e.Key]; ok {
			s.server[e.Key] = e
		} else if raw, err := mconfig.FileValue(e.Key); err == nil {
			_ = mconfig.Set(e.Key, raw)
		}
		if s.remote == nil && effectOf(e.Key) == applyReload {
			s.reload()
		}
		h.cursor = 0
		cmd := s.openHistory(ctx, h.key)
		return tea.Batch(cmd, base.Notify(fmt.Sprintf("%s set back to %v (undo of change %d)", e.Key, e.Value, c.ID)))
	}
	return nil
}

func (s *state) viewHistory() string {
	h := s.history
	title := "Settings history"
	if h.key != "" {
		title += ": " + h.key
	}
	lines := []string{style.Bold.Accent.Render(title), ""}
	if len(h.changes) == 0 {
		lines = append(lines, style.Normal.Secondary.Render("no changes (the keys in mangal.toml have no history)"))
	}
	value := func(v *string) string {
		if v == nil {
			return "default"
		}
		return ansi.Truncate(*v, maxValueWidth, "…")
	}
	height := max(3, s.size.Height-4)
	first := max(0, min(h.cursor-height/2, len(h.changes)-height))
	for n := first; n < min(len(h.changes), first+height); n++ {
		c := h.changes[n]
		cursor := "  "
		line := fmt.Sprintf("%s  %s: %s → %s  (%s)", c.At.Format("2006-01-02 15:04"), c.Key, value(c.Old), value(c.New), c.Who)
		if n == h.cursor {
			cursor = style.Bold.Accent.Render(icon.Item.Raw()) + " "
			line = style.Bold.Base.Render(line)
		}
		lines = append(lines, cursor+line)
	}
	if s.err != "" {
		lines = append(lines, "", style.Normal.Error.Width(max(20, s.size.Width)).Render(s.err))
	}
	return strings.Join(lines, "\n")
}

// errorText is the error without the key, shown next to it.
func errorText(key string, err error) string {
	return strings.TrimPrefix(err.Error(), fmt.Sprintf("config key %q: ", key))
}

func (s *state) updateEditing(ctx context.Context, msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(msg, s.keys.save):
			cmd, ok := s.set(ctx, s.key, s.input.Raw())
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

func (s *state) updateMapping(ctx context.Context, msg tea.Msg) tea.Cmd {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if s.maps.inputting {
		if ok && key.Matches(keyMsg, s.keys.save) {
			return s.saveEntry(ctx)
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

func (s *state) removeEntry(ctx context.Context, msg removeEntryMsg) tea.Cmd {
	if s.mode != mapping || s.key != msg.key {
		return nil
	}
	entries := s.entries()
	delete(entries, msg.id)
	cmd, ok := s.set(ctx, s.key, entries)
	if ok {
		s.syncIDs()
	}
	return cmd
}

// saveEntry takes the id of a new entry, or sets the url of the entry.
func (s *state) saveEntry(ctx context.Context) tea.Cmd {
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
	cmd, ok := s.set(ctx, s.key, entries)
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
	raw, _ := s.itemOf(s.key).raw()
	m, _ := raw.(map[string]any)
	return maps.Clone(m)
}

func (s *state) syncIDs() {
	s.maps.ids = slices.Sorted(maps.Keys(s.entries()))
	s.maps.cursor = min(s.maps.cursor, max(0, len(s.maps.ids)-1))
}

func (s *state) View() string {
	return s.Host.View(func() string {
		switch s.mode {
		case mapping:
			return s.viewMap()
		case historying:
			return s.viewHistory()
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
	i := s.itemOf(key)
	desc := lipgloss.NewStyle().Width(width).Render(i.description())
	lines = append(lines, strings.Split(desc, "\n")...)
	if len(lines) > 4 {
		lines = append(lines[:3], lines[3]+"…")
	}

	var extra []string
	if !i.secret() && i.kind() != mconfig.KindMap {
		if def := i.def(); def != nil {
			extra = append(extra, fmt.Sprintf("default %v", def))
		}
	}
	if i.e != nil {
		extra = append(extra, serverDetails(*i.e)...)
	} else if hint := effectOf(key).hint(); hint != "" {
		extra = append(extra, hint)
	}
	if i.e == nil {
		extra = append(extra, storedLabel(key))
		switch src := mconfig.SourceOf(key); src.Kind {
		case "env", "flag":
			extra = append(extra, style.Normal.Warning.Render(src.Name+" overrides the stored value"))
		}
		if slices.Contains(mconfig.RestartNeeded(), key) {
			extra = append(extra, style.Normal.Warning.Render("changed since mangal started: restart it to use the new value"))
		}
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

// serverDetails are when a change of the server's key applies, where it's stored, what
// overrides it.
func serverDetails(e core.ConfigEntry) []string {
	var out []string
	switch e.Applies {
	case "restart":
		out = append(out, "applies once mangal serve restarts")
	case "reload":
		out = append(out, "applies once the server loads its providers again")
	}
	if e.Stored == string(mconfig.StoredDB) {
		out = append(out, "stored in the server's database")
	} else {
		out = append(out, "stored in the server's mangal.toml")
	}
	if e.Source.Kind == "env" || e.Source.Kind == "flag" {
		out = append(out, style.Normal.Warning.Render(e.Source.Name+" overrides it on the server"))
	}
	if e.Restart {
		out = append(out, style.Normal.Warning.Render("changed since the server started: restart mangal serve to use it"))
	}
	return out
}

// storedLabel says where the key is stored.
func storedLabel(key string) string {
	if mconfig.StoredIn(key) == mconfig.StoredDB {
		return "stored in the database (shared)"
	}
	return "stored in mangal.toml (this machine)"
}
