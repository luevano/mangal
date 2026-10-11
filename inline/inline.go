package inline

import (
	"github.com/luevano/libmangal/mangadata"
	lmanilist "github.com/luevano/libmangal/metadata/anilist"
)

type QueryResult struct {
	QueryParams Args          `json:"query_params"`
	Results     []MangaResult `json:"results"`
}

// TODO: change Anilist to Metadata in general
type MangaResult struct {
	Index    int                  `json:"index"`
	Manga    mangadata.Manga      `json:"manga"`
	Chapters *[]mangadata.Chapter `json:"chapters"`
	Anilist  *lmanilist.Manga     `json:"anilist"`
	// AnilistBadMatch is the closest Anilist manga when it's too unlike the manga
	// to be used as its Anilist (then Anilist is null).
	AnilistBadMatch *BadMatch `json:"anilist_bad_match,omitempty"`
}

type BadMatch struct {
	Score   float64          `json:"score"`
	Anilist *lmanilist.Manga `json:"anilist"`
}

type Args struct {
	Query                  string `json:"query"`
	Provider               string `json:"provider"`
	MangaSelector          string `json:"manga_selector"`
	ChapterSelector        string `json:"chapter_selector"`
	ChapterPopulate        bool   `json:"chapter_populate"`
	PreferProviderMetadata bool   `json:"prefer_provider_metadata"`
	AnilistID              int    `json:"anilist_id"`
	AnilistDisable         bool   `json:"anilist_disable"`
	JSONOutput             bool   `json:"json_output,omitempty"`
}
