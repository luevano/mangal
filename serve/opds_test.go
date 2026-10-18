package serve

import (
	"bytes"
	"encoding/xml"
	"net/http"
	"strings"
	"testing"
)

type testFeed struct {
	Title   string     `xml:"title"`
	Links   []testLink `xml:"link"`
	Entries []struct {
		Title string     `xml:"title"`
		Links []testLink `xml:"link"`
	} `xml:"entry"`
}

type testLink struct {
	Rel   string `xml:"rel,attr"`
	Href  string `xml:"href,attr"`
	Type  string `xml:"type,attr"`
	Count int    `xml:"http://vaemendis.net/opds-pse/ns count,attr"`
}

func (s *testServer) feed(t *testing.T, path string) testFeed {
	t.Helper()
	code, h, body := s.get(t, path)
	if code != http.StatusOK || !strings.HasPrefix(h.Get("Content-Type"), "application/atom+xml") {
		t.Fatalf("%s: %d %v %s", path, code, h, body)
	}
	var f testFeed
	if err := xml.Unmarshal(body, &f); err != nil {
		t.Fatalf("%s: %s", path, err)
	}
	return f
}

func linkOf(links []testLink, rel string) (testLink, bool) {
	for _, l := range links {
		if l.Rel == rel {
			return l, true
		}
	}
	return testLink{}, false
}

// TestOPDS: the catalog's navigation, a manga's chapters with their files and pages
// streamed (PSE), the search.
func TestOPDS(t *testing.T) {
	s, _ := newReaderServer(t)
	root := s.feed(t, "/opds")
	if len(root.Entries) != 2 {
		t.Fatalf("root %+v", root)
	}
	lib := s.feed(t, "/opds/library")
	if len(lib.Entries) != 1 || lib.Entries[0].Title != "One-Punch Man" {
		t.Fatalf("library %+v", lib)
	}
	sub, ok := linkOf(lib.Entries[0].Links, "subsection")
	if !ok || sub.Href != "/opds/mangas/1" {
		t.Fatalf("manga link %+v", lib.Entries[0].Links)
	}
	if f := s.feed(t, "/opds/library?q=nothing"); len(f.Entries) != 0 {
		t.Errorf("search %+v", f)
	}
	manga := s.feed(t, sub.Href)
	if len(manga.Entries) != 3 {
		t.Fatalf("chapters %+v", manga)
	}
	stream, ok := linkOf(manga.Entries[0].Links, "http://vaemendis.net/opds-pse/stream")
	if !ok || stream.Count != 2 || !strings.Contains(stream.Href, "/pages/{pageNumber}?") {
		t.Fatalf("stream %+v", manga.Entries[0].Links)
	}
	if _, ok := linkOf(manga.Entries[2].Links, "http://vaemendis.net/opds-pse/stream"); ok {
		t.Errorf("a pdf streamed %+v", manga.Entries[2].Links)
	}
	page := strings.NewReplacer("{pageNumber}", "1", "{maxWidth}", "800").Replace(stream.Href)
	if code, h, body := s.get(t, page); code != http.StatusOK || h.Get("Content-Type") != "image/png" || !bytes.Equal(body, pngData) {
		t.Errorf("page %s: %d %v", page, code, h)
	}
	acq, _ := linkOf(manga.Entries[0].Links, "http://opds-spec.org/acquisition")
	if code, h, _ := s.get(t, acq.Href); code != http.StatusOK || acq.Type != "application/vnd.comicbook+zip" || h.Get("Content-Type") != acq.Type {
		t.Errorf("file %s: %d %v", acq.Href, code, h)
	}
	if code, _, _ := s.get(t, "/opds/mangas/1/cover"); code != http.StatusOK {
		t.Errorf("cover %d", code)
	}
	if f := s.feed(t, "/opds/recent"); len(f.Entries) != 3 {
		t.Errorf("recent %+v", f)
	}
	if code, _, body := s.get(t, "/opds/search.xml"); code != http.StatusOK || !strings.Contains(string(body), "/opds/library?q={searchTerms}") {
		t.Errorf("search.xml %d %s", code, body)
	}
}

// TestOPDSAuth: a reader app is asked for basic auth: a user's name and password, not an
// API token.
func TestOPDSAuth(t *testing.T) {
	s := newTestServer(t, "0123456789abcdef")
	addUser(t, s, "reader", "a long password", ScopeRead)
	resp, err := http.Get(s.URL + "/opds")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Basic") {
		t.Fatalf("nobody: %d %v", resp.StatusCode, resp.Header)
	}
	for _, tc := range []struct {
		user, password string
		want           int
	}{
		{"me", "0123456789abcdef", http.StatusUnauthorized},
		{"reader", "wrong password", http.StatusUnauthorized},
		{"reader", "a long password", http.StatusOK},
		// the second time from the cache
		{"reader", "a long password", http.StatusOK},
	} {
		req, _ := http.NewRequest("GET", s.URL+"/opds", nil)
		req.SetBasicAuth(tc.user, tc.password)
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s %q: %d, want %d", tc.user, tc.password, resp.StatusCode, tc.want)
		}
	}
	// unknown names aren't remembered (any can be sent)
	a := s.api.Auth
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.users["me"]; ok || len(a.users) != 1 {
		t.Errorf("cached users %v", a.users)
	}
}
