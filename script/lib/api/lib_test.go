package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func runLua(t *testing.T, connect Connect, script string) error {
	t.Helper()
	state := lua.NewState()
	defer state.Close()
	state.SetContext(context.Background())
	state.PreloadModule(libName, Lib(connect).Loader())
	return state.DoString(script)
}

func serverOf(t *testing.T, h http.HandlerFunc) Connect {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return func(_ context.Context, url, token string) (Conn, error) {
		if url == "" {
			url, token = srv.URL, "key"
		}
		return Conn{URL: url, Token: token, HTTP: srv.Client()}, nil
	}
}

func TestCalls(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	polls := 0
	connect := serverOf(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		asked = append(asked, r.Method+" "+r.URL.RequestURI()+" "+string(body)+" "+r.Header.Get("Authorization"))
		mu.Unlock()
		switch {
		case r.URL.Path == "/api/v1/nope":
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"error":"not for you","code":"forbidden"}`)
		case strings.HasPrefix(r.URL.Path, "/api/v1/jobs/"):
			polls++
			if polls < 2 {
				fmt.Fprint(w, `{"status":"running"}`)
				return
			}
			fmt.Fprint(w, `{"status":"done","message":"over"}`)
		case r.URL.Path == "/api/v1/library/merge" && r.URL.Query().Get("apply") == "":
			fmt.Fprint(w, `{"applied":false,"plan":{"lines":["merge"],"hash":"sha256:1"}}`)
		case r.URL.Path == "/api/v1/library/merge":
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"error":"the plan changed","code":"plan_changed","plan":{"lines":["merge"],"hash":"sha256:2"}}`)
		case r.URL.Path == "/api/v1/events":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, ": hi\n\nid: 1\ndata: {\"seq\":1,\"topic\":\"job\"}\n\nid: 2\ndata: {\"seq\":2,\"topic\":\"op\"}\n\n")
		default:
			fmt.Fprint(w, `{"title":"Berserk","list":[1,2]}`)
		}
	})
	err := runLua(t, connect, `
local api = require("api")
local m = api.get("/library/mangas/1", { q = "x y" })
assert(m.title == "Berserk" and m.list[2] == 2)
local p = api.post("/downloads", { provider = "p", chapters = "last", ids = { 1, 2 } })
assert(p.title == "Berserk")
assert(api.wait_job(7).message == "over")
local ok, e = pcall(api.get, "/nope")
assert(not ok and e.status == 403 and e.code == "forbidden" and e.message == "not for you", tostring(e))
assert(string.find(tostring(e), "not for you"), tostring(e))
local plan = api.change("library/merge", { keep = 5, other = 6 })
assert(plan.hash == "sha256:1" and plan.lines[1] == "merge")
ok, e = pcall(api.apply, "library/merge", { keep = 5, other = 6 }, plan, { force = true })
assert(not ok and e.code == "plan_changed" and e.plan.hash == "sha256:2", tostring(e))
local seen = {}
api.events({ "job", "op" }, function(ev) table.insert(seen, ev.topic); return #seen < 2 end)
assert(seen[1] == "job" and seen[2] == "op", #seen)
`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /api/v1/library/mangas/1?q=x+y  Bearer key",
		`POST /api/v1/downloads {"chapters":"last","ids":[1,2],"provider":"p"} Bearer key`,
	}
	for i, w := range want {
		if asked[i] != w {
			t.Errorf("%d: %q, want %q", i, asked[i], w)
		}
	}
	var apply string
	for _, a := range asked {
		if strings.HasPrefix(a, "POST /api/v1/library/merge?apply=true") {
			apply = a
		}
	}
	if !strings.Contains(apply, `"expect":"sha256:1"`) || !strings.Contains(apply, `"force":true`) || !strings.Contains(apply, `"keep":5`) {
		t.Errorf("apply: %q", apply)
	}
}

func TestChangeDelete(t *testing.T) {
	var got string
	connect := serverOf(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Method + " " + r.URL.RequestURI()
		fmt.Fprint(w, `{"applied":true}`)
	})
	if err := runLua(t, connect, `assert(require("api").apply("DELETE bindings", { provider = "p" }, "sha256:x").applied)`); err != nil {
		t.Fatal(err)
	}
	if got != "DELETE /api/v1/bindings?apply=true&expect=sha256%3Ax&provider=p" {
		t.Errorf("%q", got)
	}
}

func TestConnect(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()
	connect := func(_ context.Context, url, token string) (Conn, error) {
		if url == "" {
			return Conn{}, errors.New("no mangal serve")
		}
		return Conn{URL: url, Token: token, HTTP: srv.Client()}, nil
	}
	if err := runLua(t, connect, fmt.Sprintf(`local api = require("api"); api.connect{url = %q, token = "t0k"}; api.status()`, srv.URL)); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer t0k" {
		t.Errorf("auth %q", auth)
	}
}

func TestNoServer(t *testing.T) {
	connect := func(context.Context, string, string) (Conn, error) {
		return Conn{}, errors.New("no mangal serve runs for this config")
	}
	if err := runLua(t, connect, `require("api").status()`); err == nil || !strings.Contains(err.Error(), "no mangal serve") {
		t.Errorf("%v", err)
	}
}
