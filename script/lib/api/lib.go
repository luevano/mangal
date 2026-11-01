// Package api is the scripts' mangal.api: mangal serve's API v1, the same operations, errors and
// permissions as "mangal api" (mangal api --list).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	luadoc "github.com/luevano/gopher-luadoc"
	luautil "github.com/luevano/luaprovider/util"
	"github.com/luevano/mangal/v5/script/lib/util"
	lua "github.com/yuin/gopher-lua"
)

const libName = "api"

// Call sends a request to the API (path under /api/v1, or /api/...), its status and body. An
// error says it can't be sent (no server).
type Call func(ctx context.Context, method, path string, body []byte) (status int, data []byte, err error)

// pollEvery is how often wait_job asks.
const pollEvery = 500 * time.Millisecond

// Lib is the module over call; nil raises an error (the docs).
func Lib(call Call) *luadoc.Lib {
	if call == nil {
		call = func(context.Context, string, string, []byte) (int, []byte, error) {
			return 0, nil, errors.New("no mangal serve")
		}
	}
	a := api{call}
	path := &luadoc.Param{Name: "path", Description: `Under /api/v1, e.g. "/library/mangas"`, Type: luadoc.String}
	answer := &luadoc.Param{Name: "answer", Description: "The answer's JSON as a table", Type: luadoc.Any}
	withBody := func(name, method string) *luadoc.Func {
		return &luadoc.Func{
			Name:        name,
			Description: method + " to the API; an error answer raises an error",
			Value:       a.request(method),
			Params:      []*luadoc.Param{path, {Name: "body", Description: "Sent as JSON", Type: luadoc.Any, Optional: true}},
			Returns:     []*luadoc.Param{answer},
		}
	}
	return &luadoc.Lib{
		Name: libName,
		Description: "mangal serve's API: the connection's server, else this config's running mangal serve (an error without one). " +
			`A plan is what a change answers, ?apply=true with its hash as "expect" applies it; "mangal api --list" lists the operations`,
		Funcs: []*luadoc.Func{
			{
				Name:        "get",
				Description: "GET from the API; an error answer raises an error",
				Value:       a.get,
				Params:      []*luadoc.Param{path, {Name: "query", Description: "Query parameters", Type: luadoc.Any, Optional: true}},
				Returns:     []*luadoc.Param{answer},
			},
			withBody("post", http.MethodPost),
			withBody("put", http.MethodPut),
			withBody("patch", http.MethodPatch),
			withBody("delete", http.MethodDelete),
			{
				Name:        "status",
				Description: "The server's status",
				Value:       func(state *lua.LState) int { return a.send(state, http.MethodGet, "/status", nil) },
				Returns:     []*luadoc.Param{answer},
			},
			{
				Name:        "wait_job",
				Description: "Waits until the job (what a download or a scan answers) is over, its last state",
				Value:       a.waitJob,
				Params:      []*luadoc.Param{{Name: "id", Description: "The job's id", Type: luadoc.Number}},
				Returns:     []*luadoc.Param{{Name: "job", Description: "The job as a table", Type: luadoc.Any}},
			},
		},
	}
}

type api struct{ call Call }

// do sends the request, an answer that isn't a success is an error saying the server's words.
func (a api) do(state *lua.LState, method, path string, body []byte) []byte {
	status, data, err := a.call(state.Context(), method, path, body)
	util.Must(state, err)
	if status < http.StatusBadRequest {
		return data
	}
	var e struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error != "" {
		util.Must(state, fmt.Errorf("%s %s: %d %s: %s", method, path, status, e.Code, e.Error))
	}
	util.Must(state, fmt.Errorf("%s %s: %d", method, path, status))
	return nil
}

func (a api) send(state *lua.LState, method, path string, body []byte) int {
	return pushJSON(state, a.do(state, method, path, body))
}

func pushJSON(state *lua.LState, data []byte) int {
	var value any
	if len(data) != 0 {
		util.Must(state, json.Unmarshal(data, &value))
	}
	state.Push(luautil.ToLValueOrRaise(state, value))
	return 1
}

func (a api) get(state *lua.LState) int {
	path := state.CheckString(1)
	if q := state.OptTable(2, nil); q != nil {
		values := url.Values{}
		q.ForEach(func(k, v lua.LValue) { values.Set(k.String(), v.String()) })
		path += "?" + values.Encode()
	}
	return a.send(state, http.MethodGet, path, nil)
}

func (a api) request(method string) lua.LGFunction {
	return func(state *lua.LState) int {
		path := state.CheckString(1)
		var body []byte
		if v := state.Get(2); v != lua.LNil {
			value, err := luautil.FromLValue(v)
			util.Must(state, err)
			body, err = json.Marshal(value)
			util.Must(state, err)
		}
		return a.send(state, method, path, body)
	}
}

func (a api) waitJob(state *lua.LState) int {
	path := "/jobs/" + strconv.Itoa(state.CheckInt(1))
	for {
		var job struct {
			Status string `json:"status"`
		}
		data := a.do(state, http.MethodGet, path, nil)
		util.Must(state, json.Unmarshal(data, &job))
		if job.Status != "queued" && job.Status != "running" && job.Status != "waiting" {
			return pushJSON(state, data)
		}
		select {
		case <-state.Context().Done():
			util.Must(state, state.Context().Err())
		case <-time.After(pollEvery):
		}
	}
}
