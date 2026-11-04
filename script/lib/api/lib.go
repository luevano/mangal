// Package api is the scripts' mangal.api: mangal serve's API v1, the same operations, errors and
// permissions as "mangal api" (mangal api --list).
package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	luadoc "github.com/luevano/gopher-luadoc"
	luautil "github.com/luevano/luaprovider/util"
	"github.com/luevano/mangal/v5/script/lib/util"
	lua "github.com/yuin/gopher-lua"
)

const libName = "api"

// Conn is a server of the API: its address (without /api/v1), the token sent (none when
// empty) and the client that sends.
type Conn struct {
	URL   string
	Token string
	HTTP  *http.Client
}

// Connect is the API to use: the one of url with token (api.connect), the default one when
// url is empty. An error says there's none.
type Connect func(ctx context.Context, url, token string) (Conn, error)

// pollEvery is how often wait_job asks, reconnectAfter how long events waits before it
// connects again after the stream ended (the server renews it every 15 minutes).
var (
	pollEvery      = 500 * time.Millisecond
	reconnectAfter = time.Second
)

// Path is the path under /api/v1 unless it's under /api/ already.
func Path(path string) string {
	if strings.HasPrefix(path, "/api/") || path == "/api" {
		return path
	}
	return "/api/v1/" + strings.TrimPrefix(path, "/")
}

// Lib is the module over connect; nil raises an error (the docs).
func Lib(connect Connect) *luadoc.Lib {
	if connect == nil {
		connect = func(context.Context, string, string) (Conn, error) {
			return Conn{}, errors.New("no mangal serve")
		}
	}
	a := &api{connect: connect}
	path := &luadoc.Param{Name: "path", Description: `Under /api/v1, e.g. "/library/mangas"`, Type: luadoc.String}
	answer := &luadoc.Param{Name: "answer", Description: "The answer's JSON as a table", Type: luadoc.Any}
	name := &luadoc.Param{Name: "name", Description: `The change's path under /api/v1 ("library/merge"), "PUT bindings" for another method than POST`, Type: luadoc.String}
	body := &luadoc.Param{Name: "body", Description: "Sent as JSON", Type: luadoc.Any, Optional: true}
	withBody := func(name, method string) *luadoc.Func {
		return &luadoc.Func{
			Name:        name,
			Description: method + " to the API; an error answer raises an error",
			Value:       a.request(method),
			Params:      []*luadoc.Param{path, body},
			Returns:     []*luadoc.Param{answer},
		}
	}
	return &luadoc.Lib{
		Name: libName,
		Description: "mangal serve's API: api.connect's server, else --server (MANGAL_API_TOKEN), the connection's server, this config's running mangal serve, " +
			"else the API in this process (script.api_scopes permissions, --write adds download and manage; its downloads run here and the script waits for them at the end). " +
			"An error answer raises a table {status, code, message, plan, reasons, confirm} (plan_changed has the new plan); \"mangal api --list\" lists the operations",
		Funcs: []*luadoc.Func{
			{
				Name:        "connect",
				Description: "Uses this server from now on (url, token: the token saved for it by mangal connection login when none)",
				Value:       a.connectTo,
				Params:      []*luadoc.Param{{Name: "server", Description: "{url = ..., token = ...}", Type: luadoc.Any}},
			},
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
				Name:        "change",
				Description: "Plans the change (nothing is changed): its plan, with lines, warnings, blocked and hash",
				Value:       a.change,
				Params:      []*luadoc.Param{name, body},
				Returns:     []*luadoc.Param{{Name: "plan", Description: "The plan", Type: luadoc.Any}},
			},
			{
				Name:        "apply",
				Description: "Applies the change as planned: refused with plan_changed (and the new plan) when it's no longer the plan",
				Value:       a.apply,
				Params: []*luadoc.Param{name, body,
					{Name: "plan", Description: "What change answered (or its hash)", Type: luadoc.Any},
					{Name: "options", Description: `{force = true} for a forceable plan, {confirm = "empty-trash"} or "force-undo" when the plan needs it`, Type: luadoc.Any, Optional: true}},
				Returns: []*luadoc.Param{{Name: "result", Description: "{applied, plan, result, undo}", Type: luadoc.Any}},
			},
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
				Params: []*luadoc.Param{{Name: "id", Description: "The job's id", Type: luadoc.Number},
					{Name: "timeout", Description: "Seconds to wait at most (the job is then still running)", Type: luadoc.Number, Optional: true}},
				Returns: []*luadoc.Param{{Name: "job", Description: "The job as a table", Type: luadoc.Any}},
			},
			{
				Name:        "events",
				Description: "Calls fn with each event of the topics (job, op, watch, health, log) as it happens, until fn returns false or the script is cancelled",
				Value:       a.events,
				Params: []*luadoc.Param{{Name: "topics", Description: `e.g. {"job", "op"}`, Type: luadoc.Any},
					{Name: "fn", Description: "Gets the event {seq, time, topic, kind, message, data}", Type: luadoc.Func{Params: []*luadoc.Param{{Name: "event", Type: luadoc.Any}}, Returns: []*luadoc.Param{{Type: luadoc.Boolean}}}.AsType()}},
			},
		},
	}
}

type api struct {
	connect Connect
	// conn is api.connect's, else the default once found
	conn *Conn
}

func (a *api) server(state *lua.LState) Conn {
	if a.conn == nil {
		c, err := a.connect(state.Context(), "", "")
		util.Must(state, err)
		a.conn = &c
	}
	return *a.conn
}

func (a *api) connectTo(state *lua.LState) int {
	t := state.CheckTable(1)
	u := t.RawGetString("url")
	if u.Type() != lua.LTString || u.String() == "" {
		state.ArgError(1, "url expected")
	}
	token := ""
	if v := t.RawGetString("token"); v != lua.LNil {
		token = v.String()
	}
	c, err := a.connect(state.Context(), u.String(), token)
	util.Must(state, err)
	a.conn = &c
	return 0
}

// open sends the request, an answer that isn't a success raises its error.
func (a *api) open(state *lua.LState, ctx context.Context, method, path string, body []byte, header http.Header) *http.Response {
	c := a.server(state)
	req, err := http.NewRequestWithContext(ctx, method, c.URL+Path(path), bytes.NewReader(body))
	util.Must(state, err)
	maps.Copy(req.Header, header)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	res, err := c.HTTP.Do(req)
	util.Must(state, err)
	if res.StatusCode >= http.StatusBadRequest {
		data, _ := io.ReadAll(res.Body)
		res.Body.Close()
		raise(state, method, path, res.StatusCode, data)
	}
	return res
}

// do sends the request, the answer's body.
func (a *api) do(state *lua.LState, method, path string, body []byte) []byte {
	res := a.open(state, state.Context(), method, path, body, nil)
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	util.Must(state, err)
	return data
}

// raise raises the API's error as a table: status, code, message, and the answer's plan,
// reasons and confirm; tostring says it all.
func raise(state *lua.LState, method, path string, status int, data []byte) {
	var e struct {
		Error   string   `json:"error"`
		Code    string   `json:"code"`
		Reasons []string `json:"reasons"`
		Plan    any      `json:"plan"`
		Confirm string   `json:"confirm"`
	}
	_ = json.Unmarshal(data, &e)
	t := state.NewTable()
	t.RawSetString("status", lua.LNumber(status))
	t.RawSetString("code", lua.LString(e.Code))
	t.RawSetString("message", lua.LString(e.Error))
	t.RawSetString("plan", luautil.ToLValueOrRaise(state, e.Plan))
	if e.Reasons != nil {
		t.RawSetString("reasons", luautil.ToLValueOrRaise(state, e.Reasons))
	}
	if e.Confirm != "" {
		t.RawSetString("confirm", lua.LString(e.Confirm))
	}
	text := fmt.Sprintf("%s %s: %d", method, path, status)
	if e.Code != "" {
		text += " " + e.Code
	}
	if e.Error != "" {
		text += ": " + e.Error
	}
	mt := state.NewTable()
	mt.RawSetString("__tostring", state.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LString(text))
		return 1
	}))
	mt.RawSetString("__concat", state.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LString(L.ToStringMeta(L.Get(1)).String() + L.ToStringMeta(L.Get(2)).String()))
		return 1
	}))
	state.SetMetatable(t, mt)
	state.Error(t, 1)
}

func (a *api) send(state *lua.LState, method, path string, body []byte) int {
	return pushJSON(state, a.do(state, method, path, body))
}

func decode(state *lua.LState, data []byte) any {
	var value any
	if len(data) != 0 {
		util.Must(state, json.Unmarshal(data, &value))
	}
	return value
}

func pushJSON(state *lua.LState, data []byte) int {
	state.Push(luautil.ToLValueOrRaise(state, decode(state, data)))
	return 1
}

func withQuery(path string, values url.Values) string {
	if len(values) == 0 {
		return path
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + values.Encode()
}

func (a *api) get(state *lua.LState) int {
	path := state.CheckString(1)
	values := url.Values{}
	if q := state.OptTable(2, nil); q != nil {
		q.ForEach(func(k, v lua.LValue) { values.Set(k.String(), v.String()) })
	}
	return a.send(state, http.MethodGet, withQuery(path, values), nil)
}

// bodyOf is the argument as a JSON object or list (an empty table is an object), nil for nil.
func bodyOf(state *lua.LState, n int) any {
	v := state.Get(n)
	if v == lua.LNil {
		return nil
	}
	value, err := util.ToGoValue(v)
	util.Must(state, err)
	if s, ok := value.([]any); ok && len(s) == 0 {
		return map[string]any{}
	}
	return value
}

func marshal(state *lua.LState, value any) []byte {
	if value == nil {
		return nil
	}
	b, err := json.Marshal(value)
	util.Must(state, err)
	return b
}

func (a *api) request(method string) lua.LGFunction {
	return func(state *lua.LState) int {
		path := state.CheckString(1)
		return a.send(state, method, path, marshal(state, bodyOf(state, 2)))
	}
}

// changeRequest is the request of a change: name is the path, after a method (POST by
// default). A DELETE has its fields in the query.
func changeRequest(state *lua.LState, name string, fields map[string]any, apply bool) (method, path string, body []byte) {
	method, path = http.MethodPost, name
	if m, p, ok := strings.Cut(name, " "); ok && m == strings.ToUpper(m) {
		method, path = m, strings.TrimSpace(p)
	}
	values := url.Values{}
	if apply {
		values.Set("apply", "true")
	}
	if method == http.MethodDelete {
		for k, v := range fields {
			values.Set(k, fmt.Sprint(v))
		}
		return method, withQuery(path, values), nil
	}
	return method, withQuery(path, values), marshal(state, fields)
}

func fieldsOf(state *lua.LState, n int) map[string]any {
	switch v := bodyOf(state, n).(type) {
	case nil:
		return map[string]any{}
	case map[string]any:
		return v
	}
	state.ArgError(n, "a table of fields expected")
	return nil
}

func (a *api) change(state *lua.LState) int {
	method, path, body := changeRequest(state, state.CheckString(1), fieldsOf(state, 2), false)
	var res struct {
		Plan any `json:"plan"`
	}
	util.Must(state, json.Unmarshal(a.do(state, method, path, body), &res))
	state.Push(luautil.ToLValueOrRaise(state, res.Plan))
	return 1
}

func (a *api) apply(state *lua.LState) int {
	fields := fieldsOf(state, 2)
	switch plan := state.Get(3).(type) {
	case lua.LString:
		fields["expect"] = string(plan)
	case *lua.LTable:
		hash := plan.RawGetString("hash")
		if hash.Type() != lua.LTString {
			state.ArgError(3, "a plan (with its hash) expected")
		}
		fields["expect"] = hash.String()
	default:
		state.ArgError(3, "a plan expected")
	}
	if opts := state.OptTable(4, nil); opts != nil {
		if lua.LVAsBool(opts.RawGetString("force")) {
			fields["force"] = true
		}
		if c := opts.RawGetString("confirm"); c != lua.LNil {
			fields["confirm"] = c.String()
		}
	}
	method, path, body := changeRequest(state, state.CheckString(1), fields, true)
	return a.send(state, method, path, body)
}

func (a *api) waitJob(state *lua.LState) int {
	path := "/jobs/" + strconv.Itoa(state.CheckInt(1))
	var deadline <-chan time.Time
	if s := state.OptNumber(2, 0); s > 0 {
		deadline = time.After(time.Duration(float64(s) * float64(time.Second)))
	}
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
		case <-deadline:
			return pushJSON(state, data)
		case <-time.After(pollEvery):
		}
	}
}

func (a *api) events(state *lua.LState) int {
	var topics []string
	state.CheckTable(1).ForEach(func(_, v lua.LValue) { topics = append(topics, v.String()) })
	fn := state.CheckFunction(2)
	var since uint64
	for {
		more, last := a.stream(state, topics, since, fn)
		if !more {
			return 0
		}
		since = last
		select {
		case <-state.Context().Done():
			util.Must(state, state.Context().Err())
		case <-time.After(reconnectAfter):
		}
	}
}

// stream calls fn with the events of one stream, false once fn says so; last is the last
// event's seq.
func (a *api) stream(state *lua.LState, topics []string, since uint64, fn *lua.LFunction) (more bool, last uint64) {
	ctx, cancel := context.WithCancel(state.Context())
	defer cancel()
	values := url.Values{"topics": {strings.Join(topics, ",")}}
	if since != 0 {
		values.Set("since", strconv.FormatUint(since, 10))
	}
	res := a.open(state, ctx, http.MethodGet, withQuery("/events", values), nil, http.Header{"Accept": {"text/event-stream"}})
	defer res.Body.Close()
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	last = since
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue
		}
		var e struct {
			Seq uint64 `json:"seq"`
		}
		if json.Unmarshal([]byte(data), &e) == nil && e.Seq > last {
			last = e.Seq
		}
		state.Push(fn)
		state.Push(luautil.ToLValueOrRaise(state, decode(state, []byte(data))))
		state.Call(1, 1)
		ret := state.Get(-1)
		state.Pop(1)
		if ret == lua.LFalse {
			return false, last
		}
	}
	if err := state.Context().Err(); err != nil {
		util.Must(state, err)
	}
	return true, last
}
