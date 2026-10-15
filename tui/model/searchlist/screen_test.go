package searchlist

import (
	"reflect"
	"testing"

	_list "charm.land/bubbles/v2/list"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/model/search"
	"github.com/luevano/mangal/v5/util/cache"
)

type item string

func (i item) FilterValue() string { return string(i) }
func (i item) Title() string       { return string(i) }
func (i item) Description() string { return "" }

func TestHistory(t *testing.T) {
	var saved cache.Records
	s := New(search.New("", "", 30, 5), list.New(1, 0, "item", "items", []string{}, func(s string) _list.DefaultItem { return item(s) }), History{
		Get: func(r *cache.Records) (bool, error) {
			*r = cache.Records{{Rank: 1, Query: "one piece"}}
			return true, nil
		},
		Set: func(r cache.Records) error {
			saved = r
			return nil
		},
	})

	// Init's history load, run by hand
	s.Update(historyMsg{screen: &s, records: cache.Records{{Rank: 1, Query: "one piece"}}})
	if sugs := s.Search.SuggestionBox(); sugs == "" {
		t.Error("no suggestions")
	}

	s.Search.Focus()
	if !s.Inputting() || s.Backable() {
		t.Error("typing in the search isn't inputting")
	}
	cmd := s.Update(search.SearchMsg("hunter"))
	if cmd == nil {
		t.Fatal("history not saved")
	}
	cmd()
	if got := saved.Get(); !reflect.DeepEqual(got, []string{"one piece", "hunter"}) {
		t.Errorf("saved %v", got)
	}

	s.SetResults([]_list.Item{item("Hunter x Hunter")})
	if !s.Searched() || s.Subtitle() != "1 item" {
		t.Errorf("results: searched %v, subtitle %q", s.Searched(), s.Subtitle())
	}
}
