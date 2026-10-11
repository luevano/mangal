package util

import (
	"reflect"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestToGoValue(t *testing.T) {
	tests := []struct {
		lua  string
		want any
	}{
		{`nil`, nil},
		{`true`, true},
		{`1.5`, 1.5},
		{`"x"`, "x"},
		{`{}`, []any{}},
		{`{1, "a", true}`, []any{1.0, "a", true}},
		{`(function() local t = {}; t[2] = "b"; t[1] = "a"; return t end)()`, []any{"a", "b"}},
		{`{[1] = 1, [3] = 3}`, map[any]any{1.0: 1.0, 3.0: 3.0}},
		{`{a = 1, b = {2}}`, map[any]any{"a": 1.0, "b": []any{2.0}}},
		{`{1, x = 2}`, map[any]any{1.0: 1.0, "x": 2.0}},
	}
	for _, tt := range tests {
		t.Run(tt.lua, func(t *testing.T) {
			state := lua.NewState()
			defer state.Close()
			if err := state.DoString("return " + tt.lua); err != nil {
				t.Fatal(err)
			}
			got := ToGoValue(state.Get(-1))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}
