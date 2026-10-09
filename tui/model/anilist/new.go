package anilist

import (
	"context"
	"time"

	_list "github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/log"
	"github.com/luevano/mangal/theme/color"
	"github.com/luevano/mangal/theme/icon"
	"github.com/luevano/mangal/tui/model/help"
	"github.com/luevano/mangal/tui/model/list"
	"github.com/luevano/mangal/util/cache"
)

func New(ctx context.Context, anilist *metadata.ProviderWithCache, standalone bool) *Model {
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

	// TODO: handle cache error?
	var userHistory cache.UserHistory
	_, err := cache.GetAuthHistory(cache.AnilistAuthHistory, &userHistory)
	if err != nil {
		log.Log("error while getting auth history for Anilist")
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
		notificationDuration: 2 * time.Second,
		title:                _styles.title.Render("Anilist"),
		selectCursor:         _styles.prompt.Render(icon.Item.Raw()),
		state:                Uninitialized,
		current:              ID,
		styles:               _styles,
		keyMap:               newKeyMap(),
	}

	m.user = anilist.User()
	m.updateCurrent()
	return m
}
