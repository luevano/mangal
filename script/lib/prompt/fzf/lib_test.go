package fzf

import (
	"testing"

	"github.com/ktr0731/go-fuzzyfinder"
	lua "github.com/yuin/gopher-lua"
)

func TestFindOne(t *testing.T) {
	var shown []string
	find = func(slice any, itemFunc func(int) string, _ ...fuzzyfinder.Option) (int, error) {
		for i := range slice.([]string) {
			shown = append(shown, itemFunc(i))
		}
		return 1, nil
	}
	t.Cleanup(func() { find = fuzzyfinder.Find })

	state := lua.NewState()
	defer state.Close()
	state.SetGlobal("find_one", state.NewFunction(findOne))
	// more items than the lua stack holds: each to_string call must leave it as it was
	err := state.DoString(`
		local items = {}
		for i = 1, 20000 do items[i] = i end
		chosen = find_one(items, function(i) return "item " .. i end)
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.GetGlobal("chosen"); got != lua.LNumber(2) {
		t.Errorf("chose %v", got)
	}
	if len(shown) != 20000 || shown[0] != "item 1" {
		t.Errorf("shown %d items, first %q", len(shown), shown[0])
	}
	if top := state.GetTop(); top != 0 {
		t.Errorf("stack top %d", top)
	}

	if err := state.DoString(`find_one({1}, function(i) error("bad item") end)`); err == nil {
		t.Error("no error from to_string")
	}

	find = func(any, func(int) string, ...fuzzyfinder.Option) (int, error) { return 0, fuzzyfinder.ErrAbort }
	if err := state.DoString(`chosen = find_one({1}, tostring)`); err != nil {
		t.Fatal(err)
	}
	if got := state.GetGlobal("chosen"); got != lua.LNil {
		t.Errorf("cancelled chose %v", got)
	}
}
