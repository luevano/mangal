package search

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/tui/base"
)

func New(placeholder, initialQuery string, width, maxSuggestions int) *Model {
	input := textinput.New()
	input.Placeholder = placeholder
	if input.Placeholder == "" {
		input.Placeholder = "Search..."
	}

	input.ShowSuggestions = true
	input.Width = width
	input.Prompt = icon.Search.Colored() + " "

	initState := unsearched
	query := strings.TrimSpace(initialQuery)
	if query != "" {
		input.SetValue(query)
		initState = searched
	}

	return &Model{
		input:    input,
		state:    initState,
		query:    query,
		maxWidth: width,
		size:     base.Size{Width: width, Height: maxSuggestions},
		styles:   defaultStyles(),
		keyMap:   newKeyMap(),
	}
}
