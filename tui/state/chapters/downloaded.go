package chapters

import (
	"path/filepath"
	"slices"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/luevano/mangal/v5/util/cache"
)

// downloaded are the chapters of the manga's library folder by number and format (their
// paths): the library is the one answer to "is it downloaded", whichever provider got it.
type downloaded struct {
	client *libmangal.Client
	manga  mangadata.Manga
	paths  map[float32]map[libmangal.Format]string
}

func newDownloaded(client *libmangal.Client, manga mangadata.Manga) *downloaded {
	d := &downloaded{client: client, manga: manga}
	d.load(true)
	return d
}

// load reads the manga's downloads from the library, after checking them against the
// files when scan.
func (d *downloaded) load(scan bool) {
	d.paths = map[float32]map[libmangal.Format]string{}
	_, dataDB, err := cache.DBs()
	if err != nil {
		log.Log("library: %s", err)
		return
	}
	lib := storage.Library{DB: dataDB}
	m, ok, err := d.libraryManga(lib)
	if err != nil || !ok {
		if err != nil {
			log.Log("library: %s", err)
		}
		return
	}
	if scan {
		scanner := library.Scanner{FS: afs.Afero, Library: lib}
		if _, err := scanner.ScanManga(m, path.DownloadsDir(), config.Download.Provider.CreateDir.Get()); err != nil {
			log.Log("library scan of %q: %s", m.Title, err)
		}
	}
	downloads, err := lib.Downloads(m.ID)
	if err != nil {
		log.Log("library: %s", err)
		return
	}
	for _, dl := range downloads {
		format, err := libmangal.FormatString(dl.Format)
		if err != nil {
			continue
		}
		if d.paths[dl.Chapter] == nil {
			d.paths[dl.Chapter] = map[libmangal.Format]string{}
		}
		d.paths[dl.Chapter][format] = dl.Path
	}
}

// libraryManga is the library manga this provider's manga was downloaded into (also
// under one of the provider's old ids).
func (d *downloaded) libraryManga(lib storage.Library) (storage.Manga, bool, error) {
	info := d.client.Info()
	ids := []string{info.ID}
	for _, alias := range info.History {
		ids = append(ids, alias.ID)
	}
	for _, id := range ids {
		m, ok, err := lib.MangaBySource(id, d.manga.Info().ID)
		if err != nil || ok {
			return m, ok, err
		}
	}

	// a folder a scan found has no source until the next download links it, that
	// download goes into the folder in the provider's directory with the same metadata
	meta := d.manga.Metadata()
	if meta == nil || meta.ID().Raw == "" || meta.ID().Source == metadata.IDSourceProvider {
		return storage.Manga{}, false, nil
	}
	mangas, err := lib.Mangas()
	if err != nil {
		return storage.Manga{}, false, err
	}
	dirs := append(ids, info.Name)
	for _, alias := range info.History {
		dirs = append(dirs, alias.Name)
	}
	for _, m := range mangas {
		if len(m.Sources) != 0 || !slices.ContainsFunc(m.MetadataIDs, func(id metadata.ID) bool {
			return id.Source == meta.ID().Source && id.Raw == meta.ID().Raw
		}) {
			continue
		}
		if !config.Download.Provider.CreateDir.Get() || slices.Contains(dirs, filepath.Base(m.Parent)) {
			return m, true, nil
		}
	}
	return storage.Manga{}, false, nil
}

// path of the chapter downloaded in the format, empty if it isn't.
func (d *downloaded) path(chapter mangadata.Chapter, format libmangal.Format) string {
	return d.paths[chapter.Info().Number][format]
}
