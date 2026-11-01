package remote

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
)

// fakeServer answers path -> body; what it was asked is in asked.
func fakeServer(t *testing.T, answers map[string]string) (*Client, *[]string) {
	t.Helper()
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.Method+" "+r.URL.Path)
		body, ok := answers[r.Method+" "+r.URL.Path]
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"not for you","code":"forbidden"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if body == "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/tokens") {
			w.WriteHeader(http.StatusCreated)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c, err := New(Options{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return c, &asked
}

const trackersJSON = `[{"source":"anilist","name":"AniList","login":"code","ready":true,"account":"nick","expired":false},
{"source":"kitsu","name":"Kitsu","login":"password","ready":true},
{"source":"myanimelist","name":"MyAnimeList","login":"address","ready":false}]`

// TestAccounts: a token sees what it may, without asking for anything refused.
func TestAccounts(t *testing.T) {
	services := func(accounts []core.Account) (out []string) {
		for _, a := range accounts {
			out = append(out, a.Service+":"+a.Status)
		}
		return out
	}

	// a reader: their trackers only
	c, asked := fakeServer(t, map[string]string{
		"GET /api/v1/auth/me":     `{"name":"kid","kind":"user","scopes":["read","progress"],"admin":false,"hide_unavailable":true}`,
		"GET /api/v1/me/accounts": trackersJSON,
	})
	list, err := remoteAccounts{c}.List(t.Context(), core.AccountsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"AniList:logged in", "Kitsu:logged out", "MyAnimeList:not set up"}; !slices.Equal(services(list), want) {
		t.Errorf("reader's rows %q", services(list))
	}
	if list[0].Metadata != metadata.IDSourceAnilist || list[0].User != "nick" {
		t.Errorf("row %+v", list[0])
	}
	if slices.Contains(*asked, "GET /api/v1/accounts") {
		t.Errorf("a reader asked for the server's accounts: %q", *asked)
	}

	// an admin's token of no user: no tracker accounts, not asked
	c, asked = fakeServer(t, map[string]string{
		"GET /api/v1/auth/me":  `{"name":"cli","kind":"token","scopes":["read","progress","admin"],"admin":true,"hide_unavailable":true}`,
		"GET /api/v1/accounts": `[{"service":"Anilist","status":"logged in"},{"service":"MangaPlus","status":"free device"}]`,
	})
	if list, err = (remoteAccounts{c}).List(t.Context(), core.AccountsOptions{}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"Anilist:logged in", "MangaPlus:free device"}; !slices.Equal(services(list), want) {
		t.Errorf("admin token's rows %q", services(list))
	}
	if slices.Contains(*asked, "GET /api/v1/me/accounts") {
		t.Errorf("a token of no user asked for its trackers: %q", *asked)
	}

	// an admin user: their trackers instead of the server's rows of them
	c, _ = fakeServer(t, map[string]string{
		"GET /api/v1/auth/me":     `{"name":"david","kind":"user","scopes":["read","progress","admin"],"admin":true,"hide_unavailable":true}`,
		"GET /api/v1/me/accounts": trackersJSON,
		"GET /api/v1/accounts":    `[{"service":"Anilist","status":"logged in as someone"},{"service":"MangaPlus","status":"free device"}]`,
	})
	if list, err = (remoteAccounts{c}).List(t.Context(), core.AccountsOptions{}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"AniList:logged in", "Kitsu:logged out", "MyAnimeList:not set up", "MangaPlus:free device"}; !slices.Equal(services(list), want) {
		t.Errorf("admin user's rows %q", services(list))
	}
}

func TestUsersAndTokens(t *testing.T) {
	c, asked := fakeServer(t, map[string]string{
		"GET /api/v1/users":             `[{"name":"kid","perms":["read"],"role":"reader","password":true,"admin":false,"created":"2026-01-02T03:04:05Z","local":true}]`,
		"PATCH /api/v1/users/kid":       "",
		"POST /api/v1/users/kid/logout": "",
		"DELETE /api/v1/users/kid":      "",
		"POST /api/v1/tokens":           `{"token":{"name":"t","scopes":["read"],"role":"reader","created":"2026-01-02T03:04:05Z"},"secret":"s3"}`,
		"PATCH /api/v1/tokens/t":        "",
		"DELETE /api/v1/tokens/t":       "",
	})
	u := remoteUsers{c}
	ctx := t.Context()
	list, err := u.List(ctx)
	if err != nil || len(list) != 1 || !list[0].Local || list[0].Role != "reader" {
		t.Fatalf("list %+v %v", list, err)
	}
	for name, err := range map[string]error{
		"password":  u.SetPassword(ctx, "kid", "a long password"),
		"perms":     u.SetPerms(ctx, "kid", []string{"read"}),
		"rename":    u.Rename(ctx, "kid", "kid2"),
		"use local": u.UseLocal(ctx, "kid"),
		"end":       u.EndLogins(ctx, "kid"),
		"remove":    u.Remove(ctx, "kid"),
		"token":     u.SetTokenScopes(ctx, "t", []string{"read"}),
		"no token":  u.RemoveToken(ctx, "t"),
	} {
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if tok, secret, err := u.AddToken(ctx, "t", "", []string{"read"}); err != nil || tok.Name != "t" || secret != "s3" {
		t.Errorf("add token %+v %v", tok, err)
	}
	if _, err := u.Tokens(ctx); !errors.Is(err, ErrForbidden) {
		t.Errorf("refused: %v (%q)", err, *asked)
	}
}

// TestKinds: a refused or empty run answers the errors the Jobs screen knows.
func TestKinds(t *testing.T) {
	var conflict string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/jobs/kinds":
			_, _ = w.Write([]byte(`[{"kind":"scan","title":"Library scan","about":"x","enabled":true,"paused":false,"manual":true}]`))
		case "/api/v1/jobs/kinds/scan/run":
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "no", "code": conflict})
		case "/api/v1/jobs/kinds/scan/pause":
			_, _ = w.Write([]byte(`{"kind":"scan","title":"Library scan","about":"x","enabled":true,"paused":true,"manual":false}`))
		case "/api/v1/jobs/kinds/scan/settings":
			body, _ := io.ReadAll(r.Body)
			if string(body) != "{\"enabled\":false}\n" && string(body) != "{\"enabled\":false}" {
				t.Errorf("settings %s", body)
			}
			_, _ = w.Write([]byte(`{"kind":"scan","title":"Library scan","about":"x","enabled":false,"paused":false,"manual":false}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c, err := New(Options{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	k := c.Kinds()
	list, err := k.List(t.Context())
	if err != nil || len(list) != 1 || list[0].Kind != jobs.KindScan {
		t.Fatalf("list %+v %v", list, err)
	}
	conflict = "refused"
	if _, err := k.Run(t.Context(), jobs.KindScan, true); !errors.Is(err, core.ErrRefused) {
		t.Errorf("refused: %v", err)
	}
	conflict = "nothing_to_do"
	if _, err := k.Run(t.Context(), jobs.KindScan, false); !errors.Is(err, core.ErrNothingToDo) {
		t.Errorf("nothing: %v", err)
	}
	if got, err := k.Pause(t.Context(), jobs.KindScan, true); err != nil || !got.Paused {
		t.Errorf("pause %+v %v", got, err)
	}
	if got, err := k.Set(t.Context(), jobs.KindScan, core.JobSettings{Enabled: new(false)}); err != nil || got.Enabled {
		t.Errorf("set %+v %v", got, err)
	}
}

// TestFill: a plan reads the same as the server's, applying keeps the job.
func TestFill(t *testing.T) {
	const plan = `{"provider":"p","manga_id":"m","manga_title":"M","from":"q","from_manga_id":"n","from_manga_title":"N","folder":"p/M",
"found":[{"number":3,"title":"Three","from_title":"3!","url":"https://q/3"}],"not_found":[],"downloaded":[],"lines":[],
"job":{"id":7,"kind":"download","title":"M: chapter 3","status":"queued","providers":["q"],"added":"2026-01-02T03:04:05Z"}}`
	c, _ := fakeServer(t, map[string]string{"POST /api/v1/providers/p/fill": plan})
	req := core.FillReq{Provider: "p", MangaID: "m", From: core.RetitleFrom{Provider: "q", MangaID: "n"}}
	got, err := c.PlanFill(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Nothing() || got.Lines()[0] != "into p/M" || got.Found[0].URL != "https://q/3" || got.FromManga.Title != "N" {
		t.Errorf("plan %+v, %q", got, got.Lines())
	}
	if err := c.ApplyFill(t.Context(), req, "chapters/p/m"); err != nil {
		t.Fatal(err)
	}
	if active := c.Jobs().Active(); len(active) != 1 || active[0].ID != 7 || active[0].Origin != "chapters/p/m" {
		t.Errorf("job %+v", active)
	}
}
