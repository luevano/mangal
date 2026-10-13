package script

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/luevano/luaprovider/util"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/provider/loader"
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

// Run runs the script with the provider's client and the mangal library.
func Run(ctx context.Context, args Args, script io.Reader) error {
	c, warnings, release, err := client.HoldByID(ctx, args.Provider)
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning: "+w)
	}
	if err != nil {
		return err
	}
	defer release()
	if err := anilist.EnsureLogin(ctx); err != nil {
		log.Debug("script: anilist without the login: %s", err)
	}

	sdkOptions, resources, err := loader.SDKOptions("script")
	if err != nil {
		return err
	}
	defer func() {
		if err := resources.Close(); err != nil {
			log.Warn("close script resources: %s", err)
		}
	}()

	state := lua.NewState()
	defer state.Close()
	state.SetContext(ctx)

	addVarsTable(state, args.Variables)
	lib.Preload(state, c, sdkOptions)
	return run(state, script)
}

// run runs the script on state, the errors the SDK raised as themselves.
func run(state *lua.LState, script io.Reader) error {
	lFunction, err := state.Load(script, "script")
	if err != nil {
		return fmt.Errorf("load script: %w", err)
	}

	err = state.CallByParam(lua.P{
		Fn:      lFunction,
		NRet:    1,
		Protect: true,
	})
	if err != nil {
		return fmt.Errorf("run script: %w", util.GoError(err))
	}
	return nil
}
