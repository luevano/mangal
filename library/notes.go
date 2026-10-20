package library

import (
	"strconv"
	"strings"
)

// Origin is where a chapter file and its title came from, as its ComicInfo.xml Notes
// line (NotesLine) and its download record (storage.Download) have it: the provider's
// manga and chapter the file was downloaded from and, when another source gave it its
// title (a fill or a retitle), that provider's manga.
type Origin struct {
	Provider, MangaID, ChapterURL string
	TitleProvider, TitleMangaID   string
}

// notesPrefix starts the Notes line mangal owns.
const notesPrefix = "mangal:"

func (o Origin) pairs() [][2]string {
	return [][2]string{
		{"provider", o.Provider}, {"manga_id", o.MangaID}, {"chapter_url", o.ChapterURL},
		{"title_provider", o.TitleProvider}, {"title_manga_id", o.TitleMangaID},
	}
}

// NotesLine is the origin as one Notes line: "mangal: key=value ...", in the order
// provider, manga_id, chapter_url, title_provider, title_manga_id, empty values left out;
// a value with a space, quote or control character is a Go quoted string.
func (o Origin) NotesLine() string {
	line := notesPrefix
	for _, kv := range o.pairs() {
		if kv[1] == "" {
			continue
		}
		v := kv[1]
		if strings.ContainsFunc(v, func(r rune) bool { return r <= ' ' || r == '"' || r == 0x7f }) {
			v = strconv.Quote(v)
		}
		line += " " + kv[0] + "=" + v
	}
	return line
}

// ParseNotes finds the mangal line in the Notes text and reads it, unknown keys ignored.
func ParseNotes(notes string) (Origin, bool) {
	for line := range strings.SplitSeq(notes, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), notesPrefix)
		if !ok {
			continue
		}
		var o Origin
		fields := map[string]*string{
			"provider": &o.Provider, "manga_id": &o.MangaID, "chapter_url": &o.ChapterURL,
			"title_provider": &o.TitleProvider, "title_manga_id": &o.TitleMangaID,
		}
		for rest = strings.TrimSpace(rest); rest != ""; rest = strings.TrimSpace(rest) {
			key, after, ok := strings.Cut(rest, "=")
			if !ok || strings.ContainsAny(key, " \t") {
				break
			}
			var value string
			if strings.HasPrefix(after, `"`) {
				quoted, err := strconv.QuotedPrefix(after)
				if err != nil {
					break
				}
				value, _ = strconv.Unquote(quoted)
				rest = after[len(quoted):]
			} else {
				value, rest, _ = strings.Cut(after, " ")
			}
			if p, ok := fields[key]; ok {
				*p = value
			}
		}
		return o, true
	}
	return Origin{}, false
}

// SetNotesLine is the Notes text with its mangal line replaced by the origin's, else the
// line put first (before what libmangal ends the Notes with); the other lines stay.
func SetNotesLine(notes string, o Origin) string {
	lines := strings.Split(notes, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), notesPrefix) {
			lines[i] = o.NotesLine()
			return strings.Join(lines, "\n")
		}
	}
	if strings.TrimSpace(notes) == "" {
		return o.NotesLine()
	}
	return o.NotesLine() + "\n\n" + notes
}
