package search

import (
	"strings"

	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/tui/util"
)

// New makes a search input up to width wide, showing up to maxSuggestions.
func New(placeholder, initialQuery string, width, maxSuggestions int) *Model {
	input := util.NewInput()
	input.Placeholder = placeholder
	if input.Placeholder == "" {
		input.Placeholder = "Search..."
	}

	input.ShowSuggestions = true
	input.SetWidth(width)
	input.Prompt = icon.Search.Colored() + " "

	initState := unsearched
	query := strings.TrimSpace(initialQuery)
	if query != "" {
		input.SetValue(query)
		initState = searched
	}

	return &Model{
		input:          input,
		state:          initState,
		query:          query,
		maxWidth:       width,
		maxSuggestions: maxSuggestions,
		styles:         defaultStyles(),
		keyMap:         newKeyMap(),
	}
}
