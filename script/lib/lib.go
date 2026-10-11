package lib

import (
	luadoc "github.com/luevano/gopher-luadoc"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata"
	sdk "github.com/luevano/luaprovider/lib"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/script/lib/anilist"
	"github.com/luevano/mangal/v5/script/lib/client"
	"github.com/luevano/mangal/v5/script/lib/json"
	"github.com/luevano/mangal/v5/script/lib/prompt"
	lua "github.com/yuin/gopher-lua"
)

func Lib(state *lua.LState, lmclient *libmangal.Client) *luadoc.Lib {
	SDKOptions := sdk.DefaultOptions()

	libs := []*luadoc.Lib{
		sdk.Lib(state, SDKOptions),
		prompt.Lib(),
		json.Lib(),
		client.Lib(lmclient),
	}

	ani, err := lmclient.GetMetadataProvider(metadata.IDSourceAnilist)
	if err == nil {
		libs = append(libs, anilist.Lib(ani))
	}

	return &luadoc.Lib{
		Name:        meta.AppName,
		Description: meta.AppName + " scripting mode utilities",
		Libs:        libs,
	}
}

func Preload(state *lua.LState, client *libmangal.Client) {
	lib := Lib(state, client)
	state.PreloadModule(lib.Name, lib.Loader())
}
