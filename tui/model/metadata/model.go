package metadata

import (
	"strconv"

	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/libmangal/metadata"
)

type Model struct {
	meta      metadata.Metadata
	metaStyle metaStyle

	ShowFull bool

	styles styles
}

// SetMetadata replaces the current metadata and updates the style.
func (m *Model) SetMetadata(meta metadata.Metadata) {
	m.meta = meta
	m.updateStyle()
}

// updateStyle sets the style based on the current metadata.
func (m *Model) updateStyle() {
	m.metaStyle = metaIDStyle(m.meta.ID())
}

func (m *Model) Title() string {
	p := string(m.meta.ID().Code)
	if p != "" {
		p = "[" + p + "] "
	}
	return p + m.metaStyle.Prefix + " Metadata"
}

func (m *Model) Color() lipgloss.Color {
	return m.metaStyle.Color
}

func (m *Model) View() string {
	var y string
	if m.meta.StartDate().Year != 0 {
		y = " (" + strconv.Itoa(m.meta.StartDate().Year) + ")"
	}

	if !m.ShowFull {
		return m.styles.mini.Background(m.metaStyle.Color).Render(m.meta.Title() + y)
	}

	i := ": "
	if id := m.meta.ID().Raw; id != "" {
		i = " (" + id + "): "
	}

	p := string(m.meta.ID().Code)
	if p != "" {
		p = "[" + p + "] "
	}
	return m.styles.mini.Background(m.metaStyle.Color).Render(p + m.metaStyle.Prefix + i + m.meta.Title() + y)
}
