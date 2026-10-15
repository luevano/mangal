package fzf

import (
	"context"

	luadoc "github.com/luevano/gopher-luadoc"
	"github.com/luevano/mangal/v5/script/lib/util"
	lua "github.com/yuin/gopher-lua"
)

const libName = "fzf"

func Lib() *luadoc.Lib {
	const generic = "T"

	return &luadoc.Lib{
		Name:        libName,
		Description: "fuzzy-finding with an fzf-like terminal user interface.",
		Funcs: []*luadoc.Func{
			{
				Name:        "find_one",
				Description: "Find displays a UI that provides fuzzy finding against the provided list",
				Value:       findOne,
				Generics:    []string{generic},
				Params: []*luadoc.Param{
					{
						Name:        "items",
						Description: "",
						Type:        luadoc.List(generic),
					},
					{
						Name:        "to_string",
						Description: "Function that is used to convert item to the string representation",
						Type: luadoc.Func{
							Name:        libName,
							Description: "",
							Params: []*luadoc.Param{
								{
									Name: "item",
									Type: generic,
								},
							},
							Returns: []*luadoc.Param{
								{
									Name: "representation",
									Type: luadoc.String,
								},
							},
						}.AsType(),
					},
				},
				Returns: []*luadoc.Param{
					{
						Name:        "item",
						Description: "The chosen item, nil if cancelled",
						Type:        generic,
					},
				},
			},
		},
	}
}

// find is swapped in tests.
var find = pick

func findOne(state *lua.LState) int {
	var values []lua.LValue
	state.CheckTable(1).ForEach(func(_, value lua.LValue) {
		values = append(values, value)
	})
	toString := state.CheckFunction(2)

	// the strings are made before, the finder can't run lua (or raise its errors)
	items := make([]string, len(values))
	for i, value := range values {
		err := state.CallByParam(lua.P{Fn: toString, NRet: 1, Protect: true}, value)
		util.Must(state, err)
		items[i] = state.Get(-1).String()
		state.Pop(1)
	}

	ctx := state.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	index, ok, err := find(ctx, items)
	util.Must(state, err)
	if !ok {
		state.Push(lua.LNil)
		return 1
	}

	state.Push(values[index])
	return 1
}
