package metadata

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/list"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/theme/icon"
	_style "github.com/luevano/mangal/v5/theme/style"
)

// TODO: handle wrapping for smaller terminals
func (m *Model) RenderMetadata() string {
	f := allFields(m.meta)

	// manually managing is prone to errros
	l := make([]string, 12)
	l[0] = jH(m.render(f.id), m.render(f.title))
	l[1] = jH(m.render(f.status), m.render(f.chapters), m.render(f.startDate), m.render(f.endDate))
	l[2] = jH(m.render(f.country), m.render(f.score), m.render(f.format), m.render(f.publisher))
	l[3] = m.render(f.url)
	l[4] = m.render(f.description)
	l[5] = m.render(f.cover)
	l[6] = m.render(f.banner)
	l[7] = m.render(f.characters)
	l[8] = jH(jV(m.render(f.genres), m.render(f.tags)), m.render(f.alternateTitles))
	l[9] = jH(m.render(f.authors), m.render(f.artists), m.render(f.translators), m.render(f.letterers))
	l[10] = m.render(f.extraIDs)
	l[11] = m.render(f.notes)

	return jV(l...)
}

func (m *Model) render(f field) string {
	var str string
	switch value := f.value.(type) {
	case string:
		str = value
	case []string:
		return m.renderList(f.name, value, _style.Normal.Secondary.Width(f.width))
	case int:
		if value != 0 {
			str = strconv.Itoa(value)
		} else {
			str = ""
		}
	case float64:
		str = strconv.FormatFloat(value, 'f', -1, 32)
	case float32:
		str = strconv.FormatFloat(float64(value), 'f', -1, 32)
	case metadata.Status:
		str = string(value)
	case metadata.Date:
		str = value.String()
	case metadata.ID:
		str = value.Raw
	case []metadata.ID:
		// convert IDS to rendered strings
		strs := make([]string, len(value))
		for i, id := range value {
			style := metaIDStyle(id)
			prefix := style.Prefix
			if id.Code != "" {
				prefix = "[" + string(id.Code) + "] " + prefix
			}
			strs[i] = lipgloss.NewStyle().Foreground(style.Color).Render(prefix+": ") + value[i].Raw
		}
		return m.renderList(f.name, strs, lipgloss.NewStyle().Width(f.width))
	default:
		str = _style.Bold.Error.Render(fmt.Sprintf("RenderField: unkown field type: %T", value))
	}
	return m.renderField(f.name, str, f.width)
}

func (m *Model) renderFieldName(name string) string {
	return m.styles.fieldName.Render(icon.Item.Raw() + name + ":")
}

func (m *Model) renderField(name string, str string, width int) string {
	minWidth := 2
	if width < minWidth {
		width = minWidth
	}
	if str == "" {
		str = _style.Normal.Secondary.Render(strings.Repeat(".", width))
	}
	return jH(m.renderFieldName(name), lipgloss.NewStyle().Width(width).MaxWidth(width).Render(str))
}

func (m *Model) renderList(name string, items []string, itemStyle lipgloss.Style) string {
	if len(items) == 0 {
		return m.renderField(name, "", 3)
	}
	l := list.New(items).
		Enumerator(m.styles.enum).
		EnumeratorStyle(m.styles.enumerator).
		ItemStyle(itemStyle)
	// jV would insert a space inbetween
	return lipgloss.JoinVertical(lipgloss.Left, m.renderFieldName(name), l.String())
}
