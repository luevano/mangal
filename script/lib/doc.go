package lib

import (
	sdk "github.com/luevano/luaprovider/lib"
	lua "github.com/yuin/gopher-lua"
)

// LuaDoc is the Lua doc of the mangal library.
func LuaDoc() string {
	state := lua.NewState()
	defer state.Close()
	return Lib(state, nil, sdk.DefaultOptions()).LuaDoc()
}
