package lib

import (
	luadoc "github.com/luevano/gopher-luadoc"
	"github.com/luevano/libmangal"
	sdk "github.com/luevano/luaprovider/lib"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/script/lib/anilist"
	"github.com/luevano/mangal/v5/script/lib/api"
	"github.com/luevano/mangal/v5/script/lib/client"
	"github.com/luevano/mangal/v5/script/lib/json"
	"github.com/luevano/mangal/v5/script/lib/prompt"
	lua "github.com/yuin/gopher-lua"
)

// Lib is the mangal library, client and call may be nil (their functions then raise an error).
func Lib(state *lua.LState, lmclient *libmangal.Client, sdkOptions *sdk.Options, call api.Call) *luadoc.Lib {
	return &luadoc.Lib{
		Name:        meta.AppName,
		Description: meta.AppName + " scripting mode utilities",
		Libs: []*luadoc.Lib{
			sdk.Lib(state, sdkOptions),
			prompt.Lib(),
			json.Lib(),
			client.Lib(lmclient),
			anilist.Lib(),
			api.Lib(call),
		},
	}
}

func Preload(state *lua.LState, client *libmangal.Client, sdkOptions *sdk.Options, call api.Call) {
	lib := Lib(state, client, sdkOptions, call)
	state.PreloadModule(lib.Name, lib.Loader())
}
