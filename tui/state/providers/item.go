package providers

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/theme/style"
)

var _ list.DefaultItem = (*item)(nil)

type item struct {
	loader libmangal.ProviderLoader
	// loadTime is when this screen loaded the provider's client
	loadTime  time.Time
	extraInfo *bool
	// checked is ticked to search it with the others ticked
	checked bool
}

func (i *item) ItemKey() string { return i.loader.Info().ID }

func (i *item) FilterValue() string {
	return i.loader.String()
}

func (i *item) Title() string {
	var title strings.Builder
	title.WriteString(i.FilterValue())
	if info := i.loader.Info(); len(info.History) != 0 {
		title.WriteString(style.Normal.Secondary.Render(" (formerly " + formerly(info) + ")"))
	}

	if i.checked {
		title.WriteString(" ")
		title.WriteString(icon.Mark.Colored())
	}

	if i.isLoaded() {
		title.WriteString(" ")
		title.WriteString(icon.Check.Colored())
		if *i.extraInfo && !i.loadTime.IsZero() {
			timeAgo := fmt.Sprintf(" loaded %s ago", time.Since(i.loadTime).Truncate(time.Second).String())
			title.WriteString(style.Normal.Secondary.Render(timeAgo))
		}
	}

	return title.String()
}

func (i *item) Description() string {
	info := i.loader.Info()
	website := info.Website
	if preferred := config.Providers.Mirrors.Get()[info.ID]; preferred != "" && preferred != info.Website {
		website = preferred
	}
	if len(info.Mirrors) != 0 {
		mirrors := "mirrors"
		if len(info.Mirrors) == 1 {
			mirrors = "mirror"
		}
		website += style.Normal.Secondary.Render(fmt.Sprintf(" (+%d %s)", len(info.Mirrors), mirrors))
	}
	if *i.extraInfo {
		moreInfo := fmt.Sprintf("%s v%s", info.ID, info.Version)
		return lipgloss.JoinVertical(lipgloss.Left, moreInfo, website)
	}

	return website
}

// formerly lists the previous names (or ids) of the provider, newest first.
func formerly(info libmangal.ProviderInfo) string {
	var names []string
	for _, alias := range info.History {
		name := alias.Name
		if name == "" || name == info.Name {
			name = alias.ID
		}
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return strings.Join(names, ", ")
}

// isLoaded is true while the provider's client is open.
func (i *item) isLoaded() bool {
	return client.Get(i.loader) != nil
}
