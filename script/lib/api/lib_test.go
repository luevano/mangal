package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func runLua(t *testing.T, call Call, script string) error {
	t.Helper()
	state := lua.NewState()
	defer state.Close()
	state.SetContext(context.Background())
	state.PreloadModule(libName, Lib(call).Loader())
	return state.DoString(script)
}

func TestCalls(t *testing.T) {
	var asked []string
	polls := 0
	call := func(_ context.Context, method, path string, body []byte) (int, []byte, error) {
		asked = append(asked, method+" "+path+" "+string(body))
		switch {
		case path == "/nope":
			return http.StatusForbidden, []byte(`{"error":"not for you","code":"forbidden"}`), nil
		case strings.HasPrefix(path, "/jobs/"):
			polls++
			if polls < 2 {
				return 200, []byte(`{"status":"running"}`), nil
			}
			return 200, []byte(`{"status":"done","message":"over"}`), nil
		}
		return 200, []byte(`{"title":"Berserk","list":[1,2]}`), nil
	}
	err := runLua(t, call, `
local api = require("api")
local m = api.get("/library/mangas/1", { q = "x y" })
assert(m.title == "Berserk" and m.list[2] == 2)
local p = api.post("/downloads", { provider = "p", chapters = "last" })
assert(p.title == "Berserk")
assert(api.wait_job(7).message == "over")
local ok, e = pcall(api.get, "/nope")
assert(not ok and string.find(tostring(e), "not for you"), tostring(e))
`)
	if err != nil {
		t.Fatal(err)
	}
	if asked[0] != "GET /library/mangas/1?q=x+y " {
		t.Errorf("get: %q", asked[0])
	}
	if !strings.Contains(asked[1], `"provider":"p"`) {
		t.Errorf("post: %q", asked[1])
	}
}

func TestNoServer(t *testing.T) {
	call := func(context.Context, string, string, []byte) (int, []byte, error) {
		return 0, nil, errors.New("no mangal serve runs for this config")
	}
	if err := runLua(t, call, `require("api").status()`); err == nil || !strings.Contains(err.Error(), "no mangal serve") {
		t.Errorf("%v", err)
	}
}
