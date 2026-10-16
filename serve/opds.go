package serve

import (
	"cmp"
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/library/pages"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// OPDS 1.2 (https://specs.opds.io/opds-1.2) with the page streaming extension (OPDS-PSE
// 1.1, https://anansi-project.github.io/docs/opds-pse/specs/v1.1) for reader apps
// (Panels, KOReader, Chunky, Tachiyomi's forks): the library as a catalog, each downloaded
// chapter to download or to read page by page. Hand-written: no maintained Go package
// has PSE. Auth is serve's: basic auth with the token as the password, the read scope.

const (
	opdsNavigation  = "application/atom+xml;profile=opds-catalog;kind=navigation"
	opdsAcquisition = "application/atom+xml;profile=opds-catalog;kind=acquisition"
	opdsPageSize    = 100
	opdsRecent      = 50
)

type opdsFeed struct {
	XMLName  xml.Name    `xml:"feed"`
	Xmlns    string      `xml:"xmlns,attr"`
	OPDS     string      `xml:"xmlns:opds,attr"`
	PSE      string      `xml:"xmlns:pse,attr"`
	DC       string      `xml:"xmlns:dcterms,attr"`
	ID       string      `xml:"id"`
	Title    string      `xml:"title"`
	Updated  string      `xml:"updated"`
	Author   opdsAuthor  `xml:"author"`
	Links    []opdsLink  `xml:"link"`
	Total    int         `xml:"opds:totalResults,omitempty"`
	Entries  []opdsEntry `xml:"entry"`
	Subtitle string      `xml:"subtitle,omitempty"`
}

type opdsAuthor struct {
	Name string `xml:"name"`
	URI  string `xml:"uri,omitempty"`
}

type opdsLink struct {
	Rel   string `xml:"rel,attr,omitempty"`
	Href  string `xml:"href,attr"`
	Type  string `xml:"type,attr,omitempty"`
	Title string `xml:"title,attr,omitempty"`
	// PSE: the pages of a stream link
	Count int `xml:"pse:count,attr,omitempty"`
}

type opdsContent struct {
	Type string `xml:"type,attr"`
	Text string `xml:",chardata"`
}

type opdsEntry struct {
	ID      string       `xml:"id"`
	Title   string       `xml:"title"`
	Updated string       `xml:"updated"`
	Content *opdsContent `xml:"content,omitempty"`
	Links   []opdsLink   `xml:"link"`
}

func newFeed(id, title, self, kind string) *opdsFeed {
	return &opdsFeed{
		Xmlns: "http://www.w3.org/2005/Atom", OPDS: "http://opds-spec.org/2010/catalog",
		PSE: "http://vaemendis.net/opds-pse/ns", DC: "http://purl.org/dc/terms/",
		ID: "urn:mangal:" + id, Title: title, Updated: opdsTime(time.Now()), Author: opdsAuthor{Name: "mangal", URI: "https://github.com/luevano/mangal"},
		Links: []opdsLink{
			{Rel: "self", Href: self, Type: kind},
			{Rel: "start", Href: "/opds", Type: opdsNavigation},
			{Rel: "search", Href: "/opds/search.xml", Type: "application/opensearchdescription+xml", Title: "Search the library"},
		},
	}
}

func opdsTime(t time.Time) string {
	if t.IsZero() {
		t = time.Unix(0, 0)
	}
	return t.UTC().Format(time.RFC3339)
}

func writeXML(c *echo.Context, contentType string, v any) error {
	b, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return c.Blob(http.StatusOK, contentType+"; charset=utf-8", append([]byte(xml.Header), b...))
}

// registerOPDS serves the catalog under /opds, behind the read scope.
func (a *API) registerOPDS(e *echo.Echo) {
	read := a.need(ScopeRead)
	e.GET("/opds", a.opdsRoot, read)
	e.GET("/opds/search.xml", a.opdsSearch, read)
	e.GET("/opds/library", a.opdsLibrary, read)
	e.GET("/opds/recent", a.opdsRecentFeed, read)
	e.GET("/opds/mangas/:id", a.opdsManga, read)
	e.GET("/opds/mangas/:id/cover", a.opdsCover, read)
	e.GET("/opds/mangas/:id/file", a.opdsFile, read)
	e.GET("/opds/mangas/:id/pages/:page", a.opdsPage, read)
}

func (a *API) opdsRoot(c *echo.Context) error {
	f := newFeed("root", "mangal", "/opds", opdsNavigation)
	now := opdsTime(time.Now())
	f.Entries = []opdsEntry{
		{ID: "urn:mangal:library", Title: "Library", Updated: now, Content: &opdsContent{Type: "text", Text: "Every manga, by title"},
			Links: []opdsLink{{Rel: "subsection", Href: "/opds/library", Type: opdsNavigation}}},
		{ID: "urn:mangal:recent", Title: "Downloaded lately", Updated: now, Content: &opdsContent{Type: "text", Text: fmt.Sprintf("The last %d chapters downloaded", opdsRecent)},
			Links: []opdsLink{{Rel: "http://opds-spec.org/sort/new", Href: "/opds/recent", Type: opdsAcquisition}}},
	}
	return writeXML(c, opdsNavigation, f)
}

func (a *API) opdsSearch(c *echo.Context) error {
	type urlTemplate struct {
		Type     string `xml:"type,attr"`
		Template string `xml:"template,attr"`
	}
	desc := struct {
		XMLName     xml.Name    `xml:"OpenSearchDescription"`
		Xmlns       string      `xml:"xmlns,attr"`
		ShortName   string      `xml:"ShortName"`
		Description string      `xml:"Description"`
		URL         urlTemplate `xml:"Url"`
	}{Xmlns: "http://a9.com/-/spec/opensearch/1.1/", ShortName: "mangal", Description: "Search the mangal library",
		URL: urlTemplate{Type: opdsNavigation, Template: "/opds/library?q={searchTerms}"}}
	return writeXML(c, "application/opensearchdescription+xml", desc)
}

func (a *API) opdsLibrary(c *echo.Context) error {
	ctx := c.Request().Context()
	svc, err := a.services(ctx)
	if err != nil {
		return err
	}
	list, err := svc.Library.Mangas(ctx, core.MangasOptions{})
	if err != nil {
		return err
	}
	mangas := list.Mangas
	q := strings.ToLower(strings.TrimSpace(c.QueryParam("q")))
	mangas = slices.DeleteFunc(mangas, func(m storage.Manga) bool {
		return m.Chapters == 0 || (q != "" && !strings.Contains(strings.ToLower(m.Title), q))
	})
	slices.SortFunc(mangas, func(x, y storage.Manga) int { return cmp.Compare(strings.ToLower(x.Title), strings.ToLower(y.Title)) })
	page, _ := strconv.Atoi(c.QueryParam("page"))
	page = max(page, 0)
	query := func(p int) string {
		v := url.Values{}
		if q != "" {
			v.Set("q", q)
		}
		if p > 0 {
			v.Set("page", strconv.Itoa(p))
		}
		if len(v) == 0 {
			return "/opds/library"
		}
		return "/opds/library?" + v.Encode()
	}
	title := "Library"
	if q != "" {
		title = fmt.Sprintf("Library: %q", q)
	}
	f := newFeed("library", title, query(page), opdsNavigation)
	f.Total = len(mangas)
	if (page+1)*opdsPageSize < len(mangas) {
		f.Links = append(f.Links, opdsLink{Rel: "next", Href: query(page + 1), Type: opdsNavigation})
	}
	if page > 0 {
		f.Links = append(f.Links, opdsLink{Rel: "previous", Href: query(page - 1), Type: opdsNavigation})
	}
	start := min(page*opdsPageSize, len(mangas))
	for _, m := range mangas[start:min(start+opdsPageSize, len(mangas))] {
		base := fmt.Sprintf("/opds/mangas/%d", m.ID)
		content := fmt.Sprintf("%d chapters", m.Chapters)
		if m.Status != "" {
			content += ", " + strings.ReplaceAll(m.Status, "_", " ")
		}
		f.Entries = append(f.Entries, opdsEntry{ID: fmt.Sprintf("urn:mangal:manga:%d", m.ID), Title: m.Title, Updated: opdsTime(m.StatusUpdated),
			Content: &opdsContent{Type: "text", Text: content},
			Links: []opdsLink{
				{Rel: "subsection", Href: base, Type: opdsAcquisition},
				{Rel: "http://opds-spec.org/image", Href: base + "/cover", Type: "image/jpeg"},
				{Rel: "http://opds-spec.org/image/thumbnail", Href: base + "/cover", Type: "image/jpeg"},
			}})
	}
	return writeXML(c, opdsNavigation, f)
}

// chapterEntry is a downloaded chapter: its file, and its pages streamed (not a PDF).
func chapterEntry(svcFS pagesOpener, m storage.Manga, d storage.Download) opdsEntry {
	base := fmt.Sprintf("/opds/mangas/%d", m.ID)
	path := url.Values{"path": {d.Path}}.Encode()
	title := "Chapter " + strconv.FormatFloat(float64(d.Chapter), 'f', -1, 32)
	if d.Volume != 0 {
		title = "Vol. " + strconv.FormatFloat(float64(d.Volume), 'f', -1, 32) + " " + title
	}
	if d.ChapterTitle != "" && !strings.EqualFold(d.ChapterTitle, title) {
		title += ": " + d.ChapterTitle
	}
	e := opdsEntry{ID: "urn:mangal:download:" + url.PathEscape(d.Path), Title: title, Updated: opdsTime(d.Downloaded),
		Content: &opdsContent{Type: "text", Text: m.Title}}
	format, _ := libmangal.FormatOf(d.Path)
	contentType := map[libmangal.Format]string{libmangal.FormatCBZ: "application/vnd.comicbook+zip", libmangal.FormatPDF: "application/pdf",
		libmangal.FormatZIP: "application/zip", libmangal.FormatTAR: "application/x-tar", libmangal.FormatTARGZ: "application/gzip"}[format]
	if contentType == "" {
		// a folder, sent as a zip
		contentType = "application/zip"
	}
	e.Links = append(e.Links, opdsLink{Rel: "http://opds-spec.org/acquisition", Href: base + "/file?" + path, Type: contentType},
		opdsLink{Rel: "http://opds-spec.org/image/thumbnail", Href: base + "/cover", Type: "image/jpeg"})
	if format != libmangal.FormatPDF {
		count := d.Pages
		if count == 0 {
			if ch, err := svcFS(d.Path); err == nil {
				count = len(ch.Pages)
			}
		}
		if count > 0 {
			// the templates' braces are PSE's, not escaped
			e.Links = append(e.Links, opdsLink{Rel: "http://vaemendis.net/opds-pse/stream", Type: "image/jpeg", Count: count,
				Href: base + "/pages/{pageNumber}?" + path + "&width={maxWidth}"})
		}
	}
	return e
}

type pagesOpener func(path string) (*pages.Chapter, error)

func (a *API) opener(ctx context.Context) (pagesOpener, error) {
	svc, err := a.services(ctx)
	if err != nil {
		return nil, err
	}
	fsys := svc.Env().FS
	return func(path string) (*pages.Chapter, error) { return pages.Open(fsys, path) }, nil
}

func (a *API) opdsManga(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return badRequest("id: %w", err)
	}
	svc, err := a.services(ctx)
	if err != nil {
		return err
	}
	m, err := svc.Library.Manga(ctx, id)
	if err != nil {
		return err
	}
	downloads, err := svc.Library.Downloads(ctx, m.ID)
	if err != nil {
		return err
	}
	open, err := a.opener(ctx)
	if err != nil {
		return err
	}
	slices.SortFunc(downloads, func(x, y storage.Download) int {
		return cmp.Or(cmp.Compare(x.Volume, y.Volume), cmp.Compare(x.Chapter, y.Chapter), cmp.Compare(x.Path, y.Path))
	})
	f := newFeed(fmt.Sprintf("manga:%d", m.ID), m.Title, fmt.Sprintf("/opds/mangas/%d", m.ID), opdsAcquisition)
	f.Links = append(f.Links, opdsLink{Rel: "up", Href: "/opds/library", Type: opdsNavigation},
		opdsLink{Rel: "http://opds-spec.org/image", Href: fmt.Sprintf("/opds/mangas/%d/cover", m.ID), Type: "image/jpeg"})
	for _, d := range downloads {
		f.Entries = append(f.Entries, chapterEntry(open, m, d))
	}
	return writeXML(c, opdsAcquisition, f)
}

func (a *API) opdsRecentFeed(c *echo.Context) error {
	ctx := c.Request().Context()
	svc, err := a.services(ctx)
	if err != nil {
		return err
	}
	list, err := svc.Library.Mangas(ctx, core.MangasOptions{})
	if err != nil {
		return err
	}
	mangas := list.Mangas
	type item struct {
		m storage.Manga
		d storage.Download
	}
	var items []item
	for _, m := range mangas {
		downloads, err := svc.Library.Downloads(ctx, m.ID)
		if err != nil {
			return err
		}
		for _, d := range downloads {
			items = append(items, item{m, d})
		}
	}
	slices.SortFunc(items, func(x, y item) int { return y.d.Downloaded.Compare(x.d.Downloaded) })
	open, err := a.opener(ctx)
	if err != nil {
		return err
	}
	f := newFeed("recent", "Downloaded lately", "/opds/recent", opdsAcquisition)
	for _, it := range items[:min(len(items), opdsRecent)] {
		e := chapterEntry(open, it.m, it.d)
		e.Title = it.m.Title + " · " + e.Title
		f.Entries = append(f.Entries, e)
	}
	return writeXML(c, opdsAcquisition, f)
}

// the files of the catalog are the API's, under /opds so a client's basic auth (which
// some send only under the catalog's path) reaches them

func opdsID(c *echo.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return 0, badRequest("id: %w", err)
	}
	return id, nil
}

func (a *API) opdsCover(c *echo.Context) error {
	id, err := opdsID(c)
	if err != nil {
		return err
	}
	res, err := a.GetLibraryCover(c.Request().Context(), apiv1.GetLibraryCoverRequestObject{Id: id})
	if err != nil {
		return err
	}
	return res.VisitGetLibraryCoverResponse(c.Response())
}

func (a *API) opdsFile(c *echo.Context) error {
	id, err := opdsID(c)
	if err != nil {
		return err
	}
	res, err := a.GetChapterFile(c.Request().Context(), apiv1.GetChapterFileRequestObject{Id: id, Params: apiv1.GetChapterFileParams{Path: c.QueryParam("path")}})
	if err != nil {
		return err
	}
	return res.VisitGetChapterFileResponse(c.Response())
}

func (a *API) opdsPage(c *echo.Context) error {
	id, err := opdsID(c)
	if err != nil {
		return err
	}
	page, err := strconv.Atoi(c.Param("page"))
	if err != nil {
		return badRequest("page: %w", err)
	}
	res, err := a.GetChapterPage(c.Request().Context(), apiv1.GetChapterPageRequestObject{Id: id, Page: page, Params: apiv1.GetChapterPageParams{Path: c.QueryParam("path")}})
	if err != nil {
		return err
	}
	return res.VisitGetChapterPageResponse(c.Response())
}
