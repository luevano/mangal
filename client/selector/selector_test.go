package selector

import (
	"errors"
	"slices"
	"testing"

	"github.com/luevano/libmangal/mangadata"
)

type manga struct {
	mangadata.Manga
	title string
}

func (m manga) Info() mangadata.MangaInfo { return mangadata.MangaInfo{Title: m.title} }

type chapter struct {
	mangadata.Chapter
	number float32
}

func (c chapter) Info() mangadata.ChapterInfo { return mangadata.ChapterInfo{Number: c.number} }

func TestMangas(t *testing.T) {
	mangas := []mangadata.Manga{manga{title: "Solo Leveling"}, manga{title: "Solo Leveling: Ragnarok"}, manga{title: "Other"}}
	tests := []struct {
		sel, query string
		mangas     []mangadata.Manga
		want       []int
	}{
		{"all", "solo", mangas, []int{0, 1, 2}},
		{"first", "solo", mangas, []int{0}},
		{"last", "solo", mangas, []int{2}},
		{" 1 ", "solo", mangas, []int{1}},
		{"exact", "solo leveling", mangas, []int{0}},
		{"exact", "solo", mangas, nil},
		{"closest", "ragnarok", mangas, []int{1}},
		{"closest", "zzz", mangas, nil},
		{"3", "solo", mangas, nil},
		{"-1", "solo", mangas, nil},
		{"bad", "solo", mangas, nil},
		{"first", "solo", nil, nil},
		// an id query picks the only result
		{"all", "mid: 123", mangas[:1], []int{0}},
		{"id", "manga-id: 123", mangas[:1], []int{0}},
		{"id", "mid: 123", mangas, nil},
		{"first", "mid: 123", mangas[:1], nil},
		// "id" inside a title isn't an id query
		{"all", "humid: x", mangas, []int{0, 1, 2}},
	}
	for _, tt := range tests {
		got, err := Mangas(tt.sel, tt.query, tt.mangas)
		if tt.want == nil {
			var selErr *Error
			if !errors.As(err, &selErr) {
				t.Errorf("Mangas(%q, %q) = %v, %v; want a selector error", tt.sel, tt.query, got, err)
			}
			continue
		}
		if err != nil || !slices.Equal(got, tt.want) {
			t.Errorf("Mangas(%q, %q) = %v, %v; want %v", tt.sel, tt.query, got, err, tt.want)
		}
	}
}

func TestMangasIDError(t *testing.T) {
	mangas := []mangadata.Manga{manga{}, manga{}, manga{}}
	_, err := Mangas("id", "mid: 1", mangas)
	if err == nil || err.Error() != `invalid manga selector "id" (3 mangas found by id, expected 1)` {
		t.Errorf("error = %v", err)
	}
}

func TestChapters(t *testing.T) {
	var chapters []mangadata.Chapter
	for _, n := range []float32{1, 2, 2.5, 3, 3, 10} {
		chapters = append(chapters, chapter{number: n})
	}
	tests := []struct {
		sel  string
		want []float32
	}{
		{"all", []float32{1, 2, 2.5, 3, 3, 10}},
		{"first", []float32{1}},
		{"last", []float32{10}},
		{"2.5", []float32{2.5}},
		{"3", []float32{3, 3}},
		{"2-3", []float32{2, 2.5, 3, 3}},
		{"-2", []float32{1, 2}},
		{"3-", []float32{3, 3, 10}},
		{" 2 - 3 ", []float32{2, 2.5, 3, 3}},
		{"11", nil},
		{"0", nil},
		{"-", nil},
		{"3-2", nil},
		{"2-11", nil},
		{"11-", nil},
		{"a-b", nil},
		{"x", nil},
	}
	for _, tt := range tests {
		got, err := Chapters(tt.sel, chapters)
		if tt.want == nil {
			var selErr *Error
			if !errors.As(err, &selErr) {
				t.Errorf("Chapters(%q) = %v, %v; want a selector error", tt.sel, got, err)
			}
			continue
		}
		var numbers []float32
		for _, c := range got {
			numbers = append(numbers, c.Info().Number)
		}
		if err != nil || !slices.Equal(numbers, tt.want) {
			t.Errorf("Chapters(%q) = %v, %v; want %v", tt.sel, numbers, err, tt.want)
		}
	}
}

func TestChaptersEmpty(t *testing.T) {
	if _, err := Chapters("first", nil); !errors.Is(err, ErrNoChapters) {
		t.Errorf("error = %v, want ErrNoChapters", err)
	}
}
