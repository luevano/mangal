package script

import (
	"context"
	"io"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/script/lib"
	lua "github.com/yuin/gopher-lua"
)

type Variables = map[string]string

type Args struct {
	File      string
	String    string
	Stdin     bool
	Provider  string
	Variables Variables
}

func addVarsTable(state *lua.LState, variables Variables) {
	table := state.NewTable()
	for key, value := range variables {
		table.RawSetString(key, lua.LString(value))
	}

	state.SetGlobal("Vars", table)
}

func addClient(state *lua.LState, client *libmangal.Client) {
	lib.Preload(state, client)
}

func Run(ctx context.Context, args Args, script io.Reader) error {
	client, err := client.NewClientByID(ctx, args.Provider)
	if err != nil {
		return err
	}

	state := lua.NewState()
	state.SetContext(ctx)

	addVarsTable(state, args.Variables)
	addClient(state, client)

	lFunction, err := state.Load(script, "script")
	if err != nil {
		return err
	}

	return state.CallByParam(lua.P{
		Fn:      lFunction,
		NRet:    1,
		Protect: true,
	})
}
