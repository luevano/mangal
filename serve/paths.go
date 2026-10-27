package serve

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/labstack/echo/v5"
	"github.com/luevano/mangal/v5/path"
)

// hidePaths keeps the server's directories out of the API's and OPDS' answers for who
// hasn't manage (a reader, a downloader): a file in the library is relative to
// download.path, any other server directory a placeholder ("<data>/...").
func (a *API) hidePaths(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		r := c.Request()
		id, ok := identityOf(r.Context())
		if !ok || id.has(ScopeManage) || !(strings.HasPrefix(r.URL.Path, "/api/") || isOPDS(r.URL.Path)) {
			return next(c)
		}
		w := &pathHider{ResponseWriter: c.Response(), replacer: pathReplacer()}
		c.SetResponse(w)
		return next(c)
	}
}

// pathReplacer replaces the server's directories, as JSON, XML and URL queries spell them.
func pathReplacer() *strings.Replacer {
	dirs := []struct{ dir, as string }{
		{path.Downloads(), ""},
		{path.Data(), "<data>"},
		{path.Cache(), "<cache>"},
		{path.Config(), "<config>"},
		{path.Logs(), "<logs>"},
		{path.Providers(), "<providers>"},
		{path.Temp(), "<temp>"},
	}
	// the longest first: one may be inside another
	slices.SortFunc(dirs, func(x, y struct{ dir, as string }) int { return len(y.dir) - len(x.dir) })
	var pairs []string
	for _, d := range dirs {
		if d.dir == "" || d.dir == string(filepath.Separator) {
			continue
		}
		dir := filepath.Clean(d.dir)
		inside, bare := d.as+"/", d.as
		if d.as == "" {
			inside, bare = "", "<library>"
		}
		js, _ := json.Marshal(dir + string(filepath.Separator))
		jsBare, _ := json.Marshal(dir)
		pairs = append(pairs,
			string(js[1:len(js)-1]), inside,
			url.QueryEscape(dir+string(filepath.Separator)), url.QueryEscape(inside),
			string(jsBare[1:len(jsBare)-1]), bare,
		)
	}
	return strings.NewReplacer(pairs...)
}

// pathHider rewrites what's written but files and images (a write is a whole JSON answer,
// an event, an OPDS feed: a path isn't cut in two).
type pathHider struct {
	http.ResponseWriter
	replacer *strings.Replacer
}

func (w *pathHider) text() bool {
	ct := w.Header().Get("Content-Type")
	return strings.Contains(ct, "json") || strings.Contains(ct, "xml") || strings.HasPrefix(ct, "text/")
}

func (w *pathHider) WriteHeader(status int) {
	if w.text() {
		// the rewritten answer is shorter
		w.Header().Del("Content-Length")
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *pathHider) Write(b []byte) (int, error) {
	if !w.text() {
		return w.ResponseWriter.Write(b)
	}
	out := w.replacer.Replace(string(b))
	if out == string(b) {
		return w.ResponseWriter.Write(b)
	}
	if _, err := w.ResponseWriter.Write([]byte(out)); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (w *pathHider) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// libraryPath is a chapter's path as the request may send it: relative to download.path
// for who doesn't see the server's paths.
func libraryPath(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(path.Downloads(), filepath.FromSlash(p))
}
