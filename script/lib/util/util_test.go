package util

import (
	"errors"
	"reflect"
	"strconv"
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
		{`{[1] = 1, [3] = 3}`, map[string]any{"1": 1.0, "3": 3.0}},
		{`{a = 1, b = {2}}`, map[string]any{"a": 1.0, "b": []any{2.0}}},
		{`{1, x = 2}`, map[string]any{"1": 1.0, "x": 2.0}},
		// a table twice isn't a cycle
		{`(function() local s = {1}; return {s, s} end)()`, []any{[]any{1.0}, []any{1.0}}},
	}
	for _, tt := range tests {
		t.Run(tt.lua, func(t *testing.T) {
			state := lua.NewState()
			defer state.Close()
			if err := state.DoString("return " + tt.lua); err != nil {
				t.Fatal(err)
			}
			got, err := ToGoValue(state.Get(-1))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestToGoValueCyclic(t *testing.T) {
	for _, code := range []string{
		`(function() local t = {}; t.self = t; return t end)()`,
		`(function() local t = {}; t[1] = {t}; return t end)()`,
		`(function() local t = {}; t[t] = 1; return t end)()`,
	} {
		state := lua.NewState()
		if err := state.DoString("return " + code); err != nil {
			t.Fatal(err)
		}
		if _, err := ToGoValue(state.Get(-1)); !errors.Is(err, ErrCyclic) {
			t.Errorf("%s: %v", code, err)
		}
		state.Close()
	}
}

func TestFloat32(t *testing.T) {
	for _, n := range []float32{10.1, 189.5, 3, 0.3} {
		if got, want := Float32(n).String(), strconv.FormatFloat(float64(n), 'f', -1, 32); got != want {
			t.Errorf("Float32(%v) = %s, want %s", n, got, want)
		}
	}
}
