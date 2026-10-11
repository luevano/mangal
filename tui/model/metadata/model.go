// Package metadata renders a manga's metadata: a badge for a status line and the details.
package metadata

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
)

// Badge is the title and year on the metadata source's color, full adds the source and
// the id.
func Badge(meta metadata.Metadata, full bool) string {
	ms := metaIDStyle(meta.ID())
	var y string
	if meta.StartDate().Year != 0 {
		y = " (" + strconv.Itoa(meta.StartDate().Year) + ")"
	}
	mini := style.Normal.Base.Padding(0, 1).Foreground(color.Bright).Background(ms.Color)
	if !full {
		return mini.Render(meta.Title() + y)
	}

	i := ": "
	if id := meta.ID().Raw; id != "" {
		i = " (" + id + "): "
	}
	return mini.Render(codePrefix(meta.ID()) + ms.Prefix + i + meta.Title() + y)
}

// Title is the title of the details, e.g. "[al] Anilist Metadata".
func Title(meta metadata.Metadata) string {
	return codePrefix(meta.ID()) + metaIDStyle(meta.ID()).Prefix + " Metadata"
}

// Color is the metadata source's color.
func Color(meta metadata.Metadata) lipgloss.Color {
	return metaIDStyle(meta.ID()).Color
}

// ShowCmd shows the details in the TUI's viewport.
func ShowCmd(meta metadata.Metadata) tea.Cmd {
	return base.Viewport(Title(meta), Details(meta), Color(meta))
}

// Status is the Badge with a trailing space for a State's status, empty without metadata.
func Status(meta metadata.Metadata, full bool) string {
	if meta == nil {
		return ""
	}
	return Badge(meta, full) + " "
}

// ShowManga shows the manga's metadata details, or says it has none.
func ShowManga(manga mangadata.Manga) tea.Cmd {
	meta := manga.Metadata()
	if meta == nil {
		return base.Notify("No metadata")
	}
	return ShowCmd(meta)
}

func codePrefix(id metadata.ID) string {
	if id.Code == "" {
		return ""
	}
	return "[" + string(id.Code) + "] "
}

// Model keeps a manga's metadata for a screen.
//
// Deprecated: use the funcs (Badge, ShowCmd) with the manga's metadata.
type Model struct {
	meta     metadata.Metadata
	ShowFull bool
}

// New is a Model of meta.
//
// Deprecated: use the funcs (Badge, ShowCmd) with the manga's metadata.
func New(meta metadata.Metadata) *Model {
	return &Model{meta: meta}
}

// SetMetadata replaces the metadata.
func (m *Model) SetMetadata(meta metadata.Metadata) {
	m.meta = meta
}

func (m *Model) Title() string            { return Title(m.meta) }
func (m *Model) Color() lipgloss.Color    { return Color(m.meta) }
func (m *Model) View() string             { return Badge(m.meta, m.ShowFull) }
func (m *Model) RenderMetadata() string   { return Details(m.meta) }
func (m *Model) ShowMetadataCmd() tea.Cmd { return ShowCmd(m.meta) }
