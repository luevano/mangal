package script

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/luevano/luaprovider/lib"
	"github.com/luevano/mangal/v5/script/lib"
	lua "github.com/yuin/gopher-lua"
)

// an error the SDK raises shows its message, not "userdata: 0x..."
func TestSDKErrorMessage(t *testing.T) {
	state := lua.NewState()
	defer state.Close()
	state.SetContext(context.Background())
	lib.Preload(state, nil, sdk.DefaultOptions(), nil)

	err := run(state, strings.NewReader(`require("mangal").sdk.encoding.base64.decode("!!not base64")`))
	if err == nil {
		t.Fatal("no error")
	}
	if msg := err.Error(); !strings.Contains(msg, "base64 decode") || strings.Contains(msg, "userdata") {
		t.Errorf("error %q", msg)
	}
}

// mangal.api's errors are tables: the script's error is their tostring
func TestTableErrorMessage(t *testing.T) {
	state := lua.NewState()
	defer state.Close()
	state.SetContext(context.Background())
	err := run(state, strings.NewReader(`error(setmetatable({ code = "forbidden" }, { __tostring = function() return "GET /x: 403 forbidden" end }))`))
	if err == nil || !strings.Contains(err.Error(), "403 forbidden") {
		t.Errorf("error %v", err)
	}
}
