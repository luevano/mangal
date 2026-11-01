package config

import (
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"strings"

	"charm.land/bubbles/v2/list"
	"github.com/charmbracelet/x/ansi"
	mconfig "github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
)

var _ list.DefaultItem = item{}

// sections in the order shown, the others after them by name.
var sections = []string{"download", "read", "providers", "metadata", "notification", "watch", "health", "jobs", "serve", "connection", "connections", "remote", "web", "tui", "cache", "data", "log", "auth", "icons", "cli"}

// summaries say what each section holds, on the section list.
var summaries = map[string]string{
	"download":     "where and how chapters are saved: path, format, names, metadata files",
	"read":         "reading from the TUI: format, history, progress sync",
	"providers":    "manga sources: filters, mirrors, FlareSolverr, MangaPlus",
	"metadata":     "Kitsu and MyAnimeList, after Anilist",
	"notification": "Discord notifications after downloads",
	"watch":        "watched mangas: check schedules, gap filling, notifications",
	"health":       "mangal serve's own health checks",
	"jobs":         "the maintenance jobs: on/off, per-provider settings, the pause between requests",
	"serve":        "where mangal serve listens",
	"connection":   "the backend the TUI and the CLI use: local or a server",
	"connections":  "the servers by name",
	"remote":       "how to reach a server: its token, CA, timeout, read cache",
	"web":          "where mangal web listens",
	"tui":          "how the TUI looks and behaves",
	"cache":        "how long searches, lists and mirrors are kept",
	"data":         "where the data database is",
	"log":          "how long log files are kept",
	"auth":         "where logins and secrets are kept",
	"icons":        "the icon set",
	"cli":          "command line output",
}

// item is a config key. Under its section's header it's indented, without the section prefix.
type item struct {
	key    string
	prefix string
	// group says whose it is on a server: "this mangal" or the server's name, "" on local
	group string
	// e is the server's key on a remote, nil for this machine's
	e *core.ConfigEntry
}

func (i item) section() string {
	if i.group != "" {
		return i.group + ": " + i.baseSection()
	}
	return i.baseSection()
}

// baseSection is the section without the group.
func (i item) baseSection() string {
	if i.e != nil && i.e.Provider != "" {
		return "providers." + i.e.Provider
	}
	return sectionOf(i.key)
}

// fromJSON is a whole JSON number as an int, as this machine's keys have it.
func fromJSON(v any) any {
	if f, ok := v.(float64); ok && f == math.Trunc(f) && math.Abs(f) < math.MaxInt32 {
		return int(f)
	}
	return v
}

// kinds are the API's kinds of a key.
var kinds = map[string]mconfig.Kind{"string": mconfig.KindString, "bool": mconfig.KindBool, "int": mconfig.KindInt, "map": mconfig.KindMap}

// The key's value, default, description, kind, values and secrecy: the server's for its key.
func (i item) raw() (any, error) {
	if i.e != nil {
		return fromJSON(i.e.Value), nil
	}
	return mconfig.Get(i.key)
}

func (i item) def() any {
	if i.e != nil {
		return fromJSON(i.e.Default)
	}
	def, _ := mconfig.Default(i.key)
	return def
}

func (i item) description() string {
	if i.e != nil {
		return i.e.Description
	}
	description, _ := mconfig.Description(i.key)
	return description
}

func (i item) kind() mconfig.Kind {
	if i.e != nil {
		return kinds[i.e.Kind]
	}
	return mconfig.KindOf(i.key)
}

func (i item) values() []string {
	if i.e != nil {
		return i.e.Values
	}
	return mconfig.Values(i.key)
}

func (i item) secret() bool {
	if i.e != nil {
		return i.e.Secret
	}
	return mconfig.IsSecret(i.key)
}

// sectionOf is the key's section: its first part, or providers.<id> for a provider's
// setting.
func sectionOf(key string) string {
	if id := mconfig.ProviderOf(key); id != "" {
		return "providers." + id
	}
	section, _, _ := strings.Cut(key, ".")
	return section
}

// FilterValue implements list.Item.
func (i item) FilterValue() string { return i.key }

// Title implements list.DefaultItem. It's plain text: the list underlines the filter
// matches by their position in the key.
func (i item) Title() string {
	title := i.key
	if i.prefix != "" {
		title = "  " + strings.TrimPrefix(i.key, i.prefix)
	}
	title += " = " + i.displayValue()
	if i.isDefault() {
		title += "  (default)"
	}
	if i.e != nil && i.e.Env != "" {
		title += "  (from " + i.e.Env + " on the server)"
	} else if name, set := mconfig.EnvVar(i.key); set && i.e == nil {
		title += "  (from " + name + ")"
	}
	return title
}

// Description implements list.DefaultItem.
func (i item) Description() string {
	description := i.description()
	if i.prefix != "" {
		description = "  " + description
	}
	return description
}

// sectionItem is a section's header, its keys listed under it when expanded.
type sectionItem struct {
	name     string
	items    []item
	expanded bool
}

// FilterValue implements list.Item.
func (i sectionItem) FilterValue() string { return i.name }

// Title implements list.DefaultItem.
func (i sectionItem) Title() string {
	arrow := "▸ "
	if i.expanded {
		arrow = "▾ "
	}
	changed := 0
	for _, key := range i.items {
		if !key.isDefault() {
			changed++
		}
	}
	title := fmt.Sprintf("%s%s  (%d keys", arrow, i.name, len(i.items))
	if len(i.items) == 1 {
		title = fmt.Sprintf("%s%s  (1 key", arrow, i.name)
	}
	if changed > 0 {
		title += fmt.Sprintf(", %d changed", changed)
	}
	return title + ")"
}

// Description implements list.DefaultItem.
func (i sectionItem) Description() string {
	name := i.name
	if n := len(i.items); n > 0 {
		name = i.items[0].baseSection()
	}
	if id, ok := strings.CutPrefix(name, "providers."); ok {
		return "the settings of the provider " + id + ", stored in the database"
	}
	return summaries[name]
}

// sectionItems are the sections in order, with their keys.
func sectionItems(keys []string) []sectionItem { return sectionsOf(sortedItems(keys)) }

// sectionsOf are the sections of the sorted items.
func sectionsOf(items []item) []sectionItem {
	var sections []sectionItem
	for _, i := range items {
		if n := len(sections); n > 0 && sections[n-1].name == i.section() {
			sections[n-1].items = append(sections[n-1].items, i)
			continue
		}
		sections = append(sections, sectionItem{name: i.section(), items: []item{i}})
	}
	return sections
}

// sortedItems are the keys by section.
func sortedItems(keys []string) []item {
	items := make([]item, len(keys))
	for n, key := range keys {
		items[n] = item{key: key}
	}
	return sortItems(items)
}

// sortItems sorts the items by section.
func sortItems(items []item) []item {
	rank := func(i item) int {
		// a provider's section after the providers'
		top, _, _ := strings.Cut(i.baseSection(), ".")
		r := slices.Index(sections, top)
		if r < 0 {
			r = len(sections)
		}
		// on a server this mangal's first, then the server's
		if i.e != nil {
			r += len(sections) + 1
		}
		return r
	}
	slices.SortStableFunc(items, func(a, b item) int {
		if d := rank(a) - rank(b); d != 0 {
			return d
		}
		if d := strings.Compare(a.section(), b.section()); d != 0 {
			return d
		}
		return strings.Compare(a.key, b.key)
	})
	return items
}

const maxValueWidth = 50

// displayValue is the key's value as shown, secrets masked.
func (i item) displayValue() string {
	raw, err := i.raw()
	if err != nil {
		return "(invalid)"
	}
	if i.secret() {
		if raw == "" {
			return `""`
		}
		return "••••••"
	}
	var s string
	switch v := raw.(type) {
	case map[string]any:
		if len(v) == 0 {
			return "(none)"
		}
		ids := slices.Sorted(maps.Keys(v))
		s = fmt.Sprintf("%d: %s", len(ids), strings.Join(ids, ", "))
	case string:
		s = `"` + v + `"`
	default:
		s = fmt.Sprint(v)
	}
	return ansi.Truncate(s, maxValueWidth, "…")
}

func (i item) isDefault() bool {
	raw, err := i.raw()
	if err != nil {
		return false
	}
	def := i.def()
	if a, ok := raw.(string); ok {
		if b, ok := def.(string); ok && i.values() != nil {
			return strings.EqualFold(a, b)
		}
	}
	return reflect.DeepEqual(raw, def)
}
