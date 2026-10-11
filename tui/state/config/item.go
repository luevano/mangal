package config

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	mconfig "github.com/luevano/mangal/v5/config"
)

var _ list.DefaultItem = item{}

// sections in the order shown, the others after them by name.
var sections = []string{"download", "read", "providers", "metadata", "notification", "tui", "cache", "auth", "icons", "cli"}

// item is a config key.
type item struct {
	key string
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
	title := i.key + " = " + displayValue(i.key)
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
	return description
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
	if len([]rune(s)) > maxValueWidth {
		s = string([]rune(s)[:maxValueWidth-1]) + "…"
	}
	return s
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
