package library

import "testing"

func TestNotesLine(t *testing.T) {
	o := Origin{Provider: "mango-mangapill", MangaID: "123/dark-gathering", ChapterURL: "https://mangapill.com/chapters/1",
		TitleProvider: "mango-mangadex", TitleMangaID: "an id with \"quotes\""}
	line := o.NotesLine()
	want := `mangal: provider=mango-mangapill manga_id=123/dark-gathering chapter_url=https://mangapill.com/chapters/1 title_provider=mango-mangadex title_manga_id="an id with \"quotes\""`
	if line != want {
		t.Fatalf("line = %s, want %s", line, want)
	}
	notes := "Translated by someone\n\nDownloaded with libmangal\nhttps://github.com/luevano/libmangal"
	set := SetNotesLine(notes, o)
	if set != line+"\n\n"+notes {
		t.Fatalf("set = %q", set)
	}
	got, ok := ParseNotes(set + "\nlater=1")
	if !ok || got != o {
		t.Fatalf("parsed %+v %v, want %+v", got, ok, o)
	}

	retitled := Origin{Provider: o.Provider, TitleProvider: "mango-mangapill", TitleMangaID: "x"}
	again := SetNotesLine(set, retitled)
	if again != retitled.NotesLine()+"\n\n"+notes {
		t.Fatalf("replaced = %q", again)
	}
	if got, _ := ParseNotes(again); got != retitled {
		t.Fatalf("parsed %+v", got)
	}
	if got, ok := ParseNotes("mangal: future=x provider=p"); !ok || got.Provider != "p" {
		t.Fatalf("unknown key: %+v %v", got, ok)
	}
	if _, ok := ParseNotes(notes); ok {
		t.Fatal("found a line in notes without one")
	}
	if SetNotesLine("", retitled) != retitled.NotesLine() {
		t.Fatal("empty notes")
	}
}
