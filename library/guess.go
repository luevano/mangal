package library

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

var (
	// the default manga name template is "Title (year) [alid-N]"
	idTag     = regexp.MustCompile(`\s*\[(al|mal|kt|mu|ap)id-([^\]]+)\]`)
	yearTag   = regexp.MustCompile(`\s*\(\d{4}\)\s*$`)
	idSources = map[metadata.IDCode]metadata.IDSource{
		metadata.IDCodeAnilist:      metadata.IDSourceAnilist,
		metadata.IDCodeMyAnimeList:  metadata.IDSourceMyAnimeList,
		metadata.IDCodeKitsu:        metadata.IDSourceKitsu,
		metadata.IDCodeMangaUpdates: metadata.IDSourceMangaUpdates,
		metadata.IDCodeAnimePlanet:  metadata.IDSourceAnimePlanet,
	}
)

// guessInfo is the MangaDirInfo of a folder without a .libmangal.json (downloaded before
// mangal wrote them): the title and metadata id from its series.json, its chapters'
// ComicInfo.xml or its name. There's no provider, the next download links it.
func (s Scanner) guessInfo(folder string, files []chapterFile) libmangal.MangaDirInfo {
	var info libmangal.MangaDirInfo
	name := filepath.Base(folder)
	for _, m := range idTag.FindAllStringSubmatch(name, -1) {
		code := metadata.IDCode(m[1])
		info.MetadataIDs = append(info.MetadataIDs, metadata.ID{Raw: m[2], Source: idSources[code], Code: code})
	}

	var series struct {
		Metadata struct {
			Name    string `json:"name"`
			ComicID int    `json:"comicid"`
			Image   string `json:"comic_image"`
		} `json:"metadata"`
	}
	if data, err := afero.ReadFile(s.FS, filepath.Join(folder, metadata.FilenameSeriesJSON)); err == nil && json.Unmarshal(data, &series) == nil {
		info.Manga.Title = series.Metadata.Name
		// comicid is the id of the metadata provider, only known by its cover
		if len(info.MetadataIDs) == 0 && series.Metadata.ComicID != 0 && strings.Contains(series.Metadata.Image, "anilist.co") {
			info.MetadataIDs = append(info.MetadataIDs, metadata.ID{Raw: strconv.Itoa(series.Metadata.ComicID), Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist})
		}
	}
	for _, f := range files {
		if info.Manga.Title != "" {
			break
		}
		info.Manga.Title = f.series
	}
	if info.Manga.Title == "" {
		info.Manga.Title = strings.TrimSpace(yearTag.ReplaceAllString(idTag.ReplaceAllString(name, ""), ""))
	}
	return info
}

// holdsMangas is true for a folder with manga folders in it (e.g. a provider directory
// when download.provider.create_dir doesn't match how the files were downloaded).
func (s Scanner) holdsMangas(folder string) bool {
	entries, err := afero.ReadDir(s.FS, folder)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		for _, name := range []string{libmangal.FilenameMangaDirInfo, metadata.FilenameSeriesJSON, metadata.FilenameCoverJPG} {
			if exists, _ := afero.Exists(s.FS, filepath.Join(folder, e.Name(), name)); exists {
				return true
			}
		}
	}
	return false
}

// sourceOf is the provider and manga id the folder's new chapters are recorded with: its
// .libmangal.json, else its first source, else the provider directory it's in.
func (s Scanner) sourceOf(m storage.Manga, folder, root string, providerDirs bool) (string, string) {
	if info, err := libmangal.ReadMangaDirInfo(s.FS, folder); err == nil {
		return info.Provider, info.Manga.ID
	}
	if len(m.Sources) != 0 {
		return m.Sources[0].Provider, m.Sources[0].MangaID
	}
	if providerDirs && m.Dir != "" && filepath.Dir(m.Parent) == filepath.Clean(root) {
		return filepath.Base(m.Parent), ""
	}
	return "", ""
}
