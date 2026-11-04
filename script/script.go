package script

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/luevano/libmangal"
	"github.com/luevano/luaprovider/util"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/core/remote"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/provider/loader"
	"github.com/luevano/mangal/v5/script/lib"
	"github.com/luevano/mangal/v5/script/lib/api"
	lua "github.com/yuin/gopher-lua"
)

type Variables = map[string]string

type Args struct {
	File      string
	String    string
	Stdin     bool
	Provider  string
	Variables Variables
	// Server is the connection's server, nil for this machine: its providers.
	Server *remote.Client
	// API is what mangal.api connects to.
	API api.Connect
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
	c, warnings, release, err := hold(ctx, args)
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
	lib.Preload(state, c, sdkOptions, args.API)
	return run(state, script)
}

// hold is the client of the provider: the server's, browsed through it, or this machine's.
func hold(ctx context.Context, args Args) (*libmangal.Client, []string, func(), error) {
	if args.Server == nil {
		return client.HoldByID(ctx, args.Provider)
	}
	loader, err := args.Server.Loader(ctx, args.Provider)
	if err != nil {
		return nil, nil, nil, err
	}
	c, release, err := client.Hold(ctx, loader)
	return c, nil, release, err
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
		// mangal.api's errors are tables, their tostring says it
		if apiErr, ok := errors.AsType[*lua.ApiError](err); ok {
			if t, ok := apiErr.Object.(*lua.LTable); ok && state.GetMetaField(t, "__tostring") != lua.LNil {
				return fmt.Errorf("run script: %s", state.ToStringMeta(t))
			}
		}
		return fmt.Errorf("run script: %w", util.GoError(err))
	}
	return nil
}
