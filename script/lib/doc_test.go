package lib

import (
	"strings"
	"testing"
)

func TestLuaDoc(t *testing.T) {
	doc := LuaDoc()
	for _, want := range []string{"search_mangas", "anilist_manga", "client_downloaded_chapter", "client_page"} {
		if !strings.Contains(doc, want) {
			t.Errorf("doc has no %q", want)
		}
	}
}
