package metadata

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/list"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/theme/icon"
	_style "github.com/luevano/mangal/v5/theme/style"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// Details renders every field of meta in width columns (0 for no limit): the values
// wrap, the fields side by side go one under the other when they don't fit.
func Details(meta metadata.Metadata, width int) string {
	m := renderer{styles: newStyles(metaIDStyle(meta.ID())), width: width}
	f := allFields(meta)

	return jV(
		m.row(m.render(f.id), m.render(f.title)),
		m.row(m.render(f.status), m.render(f.chapters), m.render(f.startDate), m.render(f.endDate)),
		m.row(m.render(f.country), m.render(f.score), m.render(f.format), m.render(f.publisher)),
		m.render(f.url),
		m.render(f.description),
		m.render(f.cover),
		m.render(f.banner),
		m.render(f.characters),
		m.row(jV(m.render(f.genres), m.render(f.tags)), m.render(f.alternateTitles)),
		m.row(m.render(f.authors), m.render(f.artists), m.render(f.translators), m.render(f.letterers)),
		m.render(f.extraIDs),
		m.render(f.notes),
	)
}

type renderer struct {
	styles styles
	// width is the columns there are, 0 for no limit
	width int
}

// row puts the fields side by side, the ones that don't fit go below.
func (m renderer) row(fields ...string) string {
	if m.width <= 0 {
		return jH(fields...)
	}
	var rows []string
	var row []string
	for _, f := range fields {
		if len(row) != 0 && lipgloss.Width(jH(append(row, f)...)) > m.width {
			rows = append(rows, jH(row...))
			row = nil
		}
		row = append(row, f)
	}
	return jV(append(rows, jH(row...))...)
}

// fit is the width of a value next to used columns, at most want.
func (m renderer) fit(want, used int) int {
	if m.width <= 0 {
		return want
	}
	return max(2, min(want, m.width-used))
}

func (m renderer) render(f field) string {
	var str string
	switch value := f.value.(type) {
	case string:
		str = value
	case []string:
		return m.renderList(f.name, value, _style.Normal.Secondary.Width(m.fit(f.width, m.listIndent())))
	case int:
		if value != 0 {
			str = strconv.Itoa(value)
		} else {
			str = ""
		}
	case float64:
		str = strconv.FormatFloat(value, 'f', -1, 32)
	case float32:
		str = stringutil.FormatFloat32(value)
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
		return m.renderList(f.name, strs, lipgloss.NewStyle().Width(m.fit(f.width, m.listIndent())))
	default:
		str = _style.Bold.Error.Render(fmt.Sprintf("RenderField: unkown field type: %T", value))
	}
	return m.renderField(f.name, str, f.width)
}

func (m renderer) renderFieldName(name string) string {
	return m.styles.fieldName.Render(icon.Item.Raw() + name + ":")
}

func (m renderer) renderField(name string, str string, width int) string {
	label := m.renderFieldName(name)
	// jH puts a space after the label
	width = m.fit(max(2, width), lipgloss.Width(label)+1)
	if str == "" {
		str = _style.Normal.Secondary.Render(strings.Repeat(".", width))
	}
	return jH(label, lipgloss.NewStyle().Width(width).MaxWidth(width).Render(str))
}

// listIndent is the width of a list's enumerator.
func (m renderer) listIndent() int {
	return lipgloss.Width(m.styles.enumerator.Render(m.styles.enum(nil, 0)))
}

func (m renderer) renderList(name string, items []string, itemStyle lipgloss.Style) string {
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
