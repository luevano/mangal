package library

import (
	"maps"
	"testing"

	"github.com/luevano/libmangal/mangadata"
)

type titledChapter struct{ info mangadata.ChapterInfo }

func (c *titledChapter) String() string              { return c.info.Title }
func (c *titledChapter) Info() mangadata.ChapterInfo { return c.info }
func (c *titledChapter) Volume() mangadata.Volume    { return nil }

// a title that's only the number gives way to one that says more
func TestChapterTitles(t *testing.T) {
	var chapters []mangadata.Chapter
	for _, info := range []mangadata.ChapterInfo{{Number: 1, Title: "Chapter 1"}, {Number: 1, Title: "Precious Moonlit Evening"}, {Number: 1, Title: "Later"},
		{Number: 2, Title: "Ch. 2"}, {Number: 3}, {Number: 3, Title: "Three"}, {Number: 4, Title: "Chapter 4.5"}, {Number: 4, Title: "x"}} {
		chapters = append(chapters, &titledChapter{info})
	}
	want := map[float32]string{1: "Precious Moonlit Evening", 2: "Ch. 2", 3: "Three", 4: "Chapter 4.5"}
	if got := ChapterTitles(chapters); !maps.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSetComicInfoTitle(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"<ComicInfo>\n  <Title>Old</Title>\n  <Number>1</Number>\n</ComicInfo>", "<ComicInfo>\n  <Title>A &amp; B</Title>\n  <Number>1</Number>\n</ComicInfo>"},
		{"<ComicInfo><Title/><Number>1</Number></ComicInfo>", "<ComicInfo><Title>A &amp; B</Title><Number>1</Number></ComicInfo>"},
		{"<?xml version=\"1.0\"?>\n<ComicInfo a=\"1\">\n  <Number>1</Number>\n</ComicInfo>", "<?xml version=\"1.0\"?>\n<ComicInfo a=\"1\">\n  <Title>A &amp; B</Title>\n  <Number>1</Number>\n</ComicInfo>"},
		// a nested Title isn't the book's
		{"<ComicInfo><Pages><Title>x</Title></Pages></ComicInfo>", "<ComicInfo>\n  <Title>A &amp; B</Title><Pages><Title>x</Title></Pages></ComicInfo>"},
	} {
		got, err := setComicInfoTitle([]byte(c.in), "A & B")
		if err != nil || string(got) != c.want {
			t.Errorf("%q:\n%s %v", c.in, got, err)
		}
	}
	if _, err := setComicInfoTitle([]byte("not xml <"), "x"); err == nil {
		t.Error("no error for broken xml")
	}
}
