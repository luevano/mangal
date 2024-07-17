package format

import (
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/config"
	"github.com/luevano/mangal/theme/icon"
	"github.com/luevano/mangal/theme/style"
)

var (
	_ list.Item        = (*item)(nil)
	_ list.DefaultItem = (*item)(nil)
)

var (
	sep  = style.Bold.Warning.Padding(0, 1).Render(icon.Separator.Raw())
	down = style.Bold.Warning.Render("down")
	read = style.Bold.Warning.Render("read")
)

// item implements list.item.
type item struct {
	format libmangal.Format
}

// FilterValue implements list.Item.
func (i *item) FilterValue() string {
	return i.format.String()
}

// Title implements list.DefaultItem.
func (i *item) Title() string {
	var sb strings.Builder
	sb.Grow(20)

	sb.WriteString(i.FilterValue())

	if i.isSelectedForDownloading() {
		sb.WriteString(sep)
		sb.WriteString(down)
	}

	if i.isSelectedForReading() {
		sb.WriteString(sep)
		sb.WriteString(read)
	}

	return sb.String()
}

// Description implements list.DefaultItem.
func (i *item) Description() string {
	ext := i.format.Extension()

	if ext == "" {
		return "<none>"
	}

	return ext
}

func (i *item) isSelectedForDownloading() bool {
	format := config.Download.Format.Get()

	return i.format == format
}

func (i *item) isSelectedForReading() bool {
	format := config.Read.Format.Get()

	return i.format == format
}