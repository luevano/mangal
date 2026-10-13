package config

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/x/ansi"
	mconfig "github.com/luevano/mangal/v5/config"
)

var _ list.DefaultItem = item{}

// sections in the order shown, the others after them by name.
var sections = []string{"download", "read", "providers", "metadata", "notification", "watch", "serve", "web", "tui", "cache", "data", "log", "auth", "icons", "cli"}

// summaries say what each section holds, on the section list.
var summaries = map[string]string{
	"download":     "where and how chapters are saved: path, format, names, metadata files",
	"read":         "reading from the TUI: format, history, progress sync",
	"providers":    "manga sources: filters, mirrors, FlareSolverr, MangaPlus",
	"metadata":     "Kitsu and MyAnimeList, after Anilist",
	"notification": "Discord notifications after downloads",
	"watch":        "watched mangas: check schedules, gap filling, notifications",
	"serve":        "where mangal serve listens",
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
}

func (i item) section() string {
	section, _, _ := strings.Cut(i.key, ".")
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
	title += " = " + displayValue(i.key)
	if isDefault(i.key) {
		title += "  (default)"
	}
	if name, set := mconfig.EnvVar(i.key); set {
		title += "  (from " + name + ")"
	}
	return title
}

// Description implements list.DefaultItem.
func (i item) Description() string {
	description, _ := mconfig.Description(i.key)
	if i.prefix != "" {
		description = "  " + description
	}
	return description
}

// sectionItem is a section's header, its keys listed under it when expanded.
type sectionItem struct {
	name     string
	keys     []string
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
	for _, key := range i.keys {
		if !isDefault(key) {
			changed++
		}
	}
	title := fmt.Sprintf("%s%s  (%d keys", arrow, i.name, len(i.keys))
	if len(i.keys) == 1 {
		title = fmt.Sprintf("%s%s  (1 key", arrow, i.name)
	}
	if changed > 0 {
		title += fmt.Sprintf(", %d changed", changed)
	}
	return title + ")"
}

// Description implements list.DefaultItem.
func (i sectionItem) Description() string { return summaries[i.name] }

// sectionItems are the sections in order, with their keys.
func sectionItems(keys []string) []sectionItem {
	var sections []sectionItem
	for _, i := range sortedItems(keys) {
		if n := len(sections); n > 0 && sections[n-1].name == i.section() {
			sections[n-1].keys = append(sections[n-1].keys, i.key)
			continue
		}
		sections = append(sections, sectionItem{name: i.section(), keys: []string{i.key}})
	}
	return sections
}

// sortedItems are the keys by section.
func sortedItems(keys []string) []item {
	items := make([]item, len(keys))
	for n, key := range keys {
		items[n] = item{key: key}
	}
	rank := func(i item) int {
		if r := slices.Index(sections, i.section()); r >= 0 {
			return r
		}
		return len(sections)
	}
	slices.SortStableFunc(items, func(a, b item) int {
		if d := rank(a) - rank(b); d != 0 {
			return d
		}
		return strings.Compare(a.key, b.key)
	})
	return items
}

const maxValueWidth = 50

// displayValue is the key's value as shown, secrets masked.
func displayValue(key string) string {
	raw, err := mconfig.Get(key)
	if err != nil {
		return "(invalid)"
	}
	if mconfig.IsSecret(key) {
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

func isDefault(key string) bool {
	raw, err := mconfig.Get(key)
	if err != nil {
		return false
	}
	def, _ := mconfig.Default(key)
	if a, ok := raw.(string); ok {
		if b, ok := def.(string); ok && mconfig.Values(key) != nil {
			return strings.EqualFold(a, b)
		}
	}
	return reflect.DeepEqual(raw, def)
}
