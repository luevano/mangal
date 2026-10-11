package util

import (
	"fmt"

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

func ToGoValue(value lua.LValue) any {
	switch value.Type() {
	case lua.LTNil:
		return nil
	case lua.LTBool:
		return bool(value.(lua.LBool))
	case lua.LTNumber:
		return float64(value.(lua.LNumber))
	case lua.LTString:
		return string(value.(lua.LString))
	case lua.LTTable:
		table := value.(*lua.LTable)

		// a sequence 1..n is a slice, anything else a map
		var count int
		table.ForEach(func(lua.LValue, lua.LValue) { count++ })
		if n := table.Len(); n == count {
			slice := make([]any, n)
			for i := range n {
				slice[i] = ToGoValue(table.RawGetInt(i + 1))
			}
			return slice
		}

		m := make(map[any]any, count)
		table.ForEach(func(key lua.LValue, value lua.LValue) {
			m[ToGoValue(key)] = ToGoValue(value)
		})
		return m
	case lua.LTUserData:
		return value.(*lua.LUserData).Value
	default:
		return nil
	}
}
