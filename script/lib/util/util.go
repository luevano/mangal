package util

import (
	"errors"
	"fmt"
	"strconv"

	lua "github.com/yuin/gopher-lua"
)

func NewUserData[T any](state *lua.LState, t T, typeName string) *lua.LUserData {
	ud := state.NewUserData()
	ud.Value = t

	state.SetMetatable(ud, state.GetTypeMetatable(typeName))

	return ud
}

func Push[T any](state *lua.LState, t T, typeName string) {
	state.Push(NewUserData(state, t, typeName))
}

func Check[T any](state *lua.LState, n int) T {
	t, ok := state.CheckUserData(n).Value.(T)
	if !ok {
		state.ArgError(n, fmt.Sprintf("%T expected", t))
	}

	return t
}

func Must(state *lua.LState, err error) {
	if err != nil {
		state.RaiseError("%s", err)
	}
}

func SliceToTable[T any](state *lua.LState, items []T, convert func(T) lua.LValue) *lua.LTable {
	table := state.NewTable()

	for _, item := range items {
		table.Append(convert(item))
	}

	return table
}

// ErrCyclic is a table that contains itself, it has no Go (JSON) value.
var ErrCyclic = errors.New("cyclic table")

// ToGoValue is the value in Go: tables are slices (a sequence 1..n) or maps with string keys
// (numbers in their shortest form, like JSON object keys).
func ToGoValue(value lua.LValue) (any, error) {
	return toGoValue(value, map[*lua.LTable]bool{})
}

// toGoValue converts value, path are the tables it's inside of.
func toGoValue(value lua.LValue, path map[*lua.LTable]bool) (any, error) {
	switch value.Type() {
	case lua.LTNil:
		return nil, nil
	case lua.LTBool:
		return bool(value.(lua.LBool)), nil
	case lua.LTNumber:
		return float64(value.(lua.LNumber)), nil
	case lua.LTString:
		return string(value.(lua.LString)), nil
	case lua.LTTable:
		table := value.(*lua.LTable)
		if path[table] {
			return nil, ErrCyclic
		}
		path[table] = true
		defer delete(path, table)

		// a sequence 1..n is a slice, anything else a map
		var count int
		table.ForEach(func(lua.LValue, lua.LValue) { count++ })
		if n := table.Len(); n == count {
			slice := make([]any, n)
			for i := range n {
				v, err := toGoValue(table.RawGetInt(i+1), path)
				if err != nil {
					return nil, err
				}
				slice[i] = v
			}
			return slice, nil
		}

		m := make(map[string]any, count)
		var err error
		table.ForEach(func(key lua.LValue, value lua.LValue) {
			if err != nil {
				return
			}
			var k, v any
			if k, err = toGoValue(key, path); err != nil {
				return
			}
			if v, err = toGoValue(value, path); err != nil {
				return
			}
			m[mapKey(k)] = v
		})
		if err != nil {
			return nil, err
		}
		return m, nil
	case lua.LTUserData:
		return value.(*lua.LUserData).Value, nil
	default:
		return nil, nil
	}
}

func mapKey(k any) string {
	switch k := k.(type) {
	case string:
		return k
	case float64:
		return strconv.FormatFloat(k, 'g', -1, 64)
	default:
		return fmt.Sprint(k)
	}
}
