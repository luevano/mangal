package inline

import (
	"fmt"

	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	lmanilist "github.com/luevano/libmangal/metadata/anilist"
)

type QueryResult struct {
	QueryParams Args          `json:"query_params"`
	Results     []MangaResult `json:"results"`
}

// MangaResult is a search result. Metadata is the manga's metadata from any source
// (MetadataSource: anilist, kitsu, mal or provider); Anilist is kept for older scripts: the
// AniList metadata only (the manga's AniList binding, or the AniList match).
type MangaResult struct {
	Index          int                  `json:"index"`
	Manga          mangadata.Manga      `json:"manga"`
	Chapters       *[]mangadata.Chapter `json:"chapters"`
	Metadata       metadata.Metadata    `json:"metadata"`
	MetadataSource string               `json:"metadata_source,omitempty"`
	Anilist        *lmanilist.Manga     `json:"anilist"`
	// AnilistBadMatch is the closest Anilist manga when it's too unlike the manga
	// to be used as its Anilist (then Anilist is null).
	AnilistBadMatch *BadMatch `json:"anilist_bad_match,omitempty"`
}

type BadMatch struct {
	Score   float64          `json:"score"`
	Anilist *lmanilist.Manga `json:"anilist"`
}

type Args struct {
	Query string `json:"query"`
	// ID is the manga's id, instead of a Query.
	ID                     string `json:"id,omitempty"`
	Provider               string `json:"provider"`
	MangaSelector          string `json:"manga_selector"`
	ChapterSelector        string `json:"chapter_selector"`
	ChapterPopulate        bool   `json:"chapter_populate"`
	PreferProviderMetadata bool   `json:"prefer_provider_metadata"`
	AnilistID              int    `json:"anilist_id"`
	AnilistDisable         bool   `json:"anilist_disable"`
	JSONOutput             bool   `json:"json_output,omitempty"`
	// Status shows what the run is doing, may be nil.
	Status Status `json:"-"`
}

// Status shows what a run is doing (cmd's status line on stderr).
type Status interface {
	// Set makes msg the status.
	Set(msg string)
	// Transient makes msg the status only where it's rewritten in place (a terminal).
	Transient(msg string)
	// Print runs print (writing to stdout or stderr) with the status out of the way.
	Print(print func())
	// Clear removes the status until the next one.
	Clear()
}

func (a Args) status(format string, v ...any) {
	if a.Status != nil {
		a.Status.Set(fmt.Sprintf(format, v...))
	}
}

func (a Args) transient(format string, v ...any) {
	if a.Status != nil {
		a.Status.Transient(fmt.Sprintf(format, v...))
	}
}

func (a Args) print(print func()) {
	if a.Status == nil {
		print()
		return
	}
	a.Status.Print(print)
}
