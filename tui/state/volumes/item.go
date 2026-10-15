package volumes

import (
	"fmt"

	"charm.land/bubbles/v2/list"
	"github.com/luevano/libmangal/mangadata"
)

var (
	_ list.Item        = (*item)(nil)
	_ list.DefaultItem = (*item)(nil)
)

// item implements list.item.
type item struct {
	volume mangadata.Volume
}

// FilterValue implements list.Item.
func (i *item) FilterValue() string {
	return fmt.Sprintf("Volume %s", i.volume)
}

// Title implements list.DefaultItem.
func (i *item) Title() string {
	return i.FilterValue()
}

// Description implements list.DefaultItem.
func (i *item) Description() string {
	return ""
}
