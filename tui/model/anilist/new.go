package anilist

import (
	"context"
	"time"

	_list "github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	mangalanilist "github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/tui/model/help"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/util/cache"
)

func New(ctx context.Context, standalone bool) *Model {
	_styles := defaultStyles()
	input := textinput.New()
	input.Width = 60
	input.PromptStyle = _styles.prompt
	input.TextStyle = _styles.text
	idInput := input
	secretInput := input
	codeInput := input

	idInput.Prompt = "ID:     "
	secretInput.Prompt = "Secret: "
	secretInput.Placeholder = "(optional)"
	codeInput.Prompt = "Code:   "

	users, err := mangalanilist.Users()
	if err != nil {
		log.Log("%s", err)
	}
	userHistory := cache.UserHistory(users)
	anilist, err := mangalanilist.Anilist()
	if err != nil {
		log.Log("%s", err)
	}

	list := list.New(1, 0, "user", "users", userHistory.Get(), func(u string) _list.DefaultItem {
		return &item{user: u}
	})
	list.SetAccentColor(color.Anilist)
	list.KeyMap.List.Filter.SetEnabled(false)
	list.KeyMap.Reverse.SetEnabled(false)
	list.KeyMap.List.GoToStart.SetEnabled(false)
	list.KeyMap.List.GoToEnd.SetEnabled(false)
	list.KeyMap.List.NextPage.SetEnabled(false)
	list.KeyMap.List.PrevPage.SetEnabled(false)

	m := &Model{
		ctx:                  ctx,
		idInput:              idInput,
		secretInput:          secretInput,
		codeInput:            codeInput,
		list:                 list,
		help:                 help.New(),
		anilist:              anilist,
		userHistory:          userHistory,
		standalone:           standalone,
		notificationDuration: 5 * time.Second,
		title:                _styles.title.Render("Anilist"),
		selectCursor:         _styles.prompt.Render(icon.Item.Raw()),
		state:                Uninitialized,
		current:              ID,
		styles:               _styles,
		keyMap:               newKeyMap(),
	}

	if anilist != nil {
		m.user = anilist.User()
	}
	m.updateCurrent()
	return m
}
