package client

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/spf13/afero"
)

// Fill gets chapters of a provider's manga (Manga) that it can't download from another
// provider (From): they go into Manga's folder as its own chapters (Chapter), recorded as
// From's (storage.Library.RecordFill), the folder staying Manga's.
type Fill struct {
	Provider libmangal.ProviderInfo
	Manga    mangadata.Manga
	From     libmangal.ProviderInfo
	// Folder is where Provider's download of Manga goes (without a volume directory),
	// MangaDir whether it's a manga folder (download.manga.create_dir).
	Folder   string
	MangaDir bool
}

// NewFill is the fill of the client's manga from the provider from, its folder the one
// the client's download of the chapter (one of the manga's) goes to.
func NewFill(ctx context.Context, c *libmangal.Client, manga mangadata.Manga, chapter mangadata.Chapter, from libmangal.ProviderInfo, options libmangal.DownloadOptions) (Fill, error) {
	dir, err := c.ChapterDirContext(ctx, chapter, options)
	if err != nil {
		return Fill{}, fmt.Errorf("folder of %q: %w", manga, err)
	}
	if options.CreateVolumeDir {
		dir = filepath.Dir(dir)
	}
	return Fill{Provider: c.Info(), Manga: manga, From: from, Folder: dir, MangaDir: options.CreateMangaDir}, nil
}

// Chapter is src, From's chapter, as Manga's: the title (its file name and ComicInfo.xml
// Title) and volume, Manga's series metadata with a ComicInfo.xml note saying where it's
// from; its URL (ComicInfo.xml Web) and pages are src's.
func (f Fill) Chapter(src mangadata.Chapter, title string, volume mangadata.VolumeInfo) mangadata.Chapter {
	info := src.Info()
	info.Title = title
	note := fmt.Sprintf("Downloaded from %s, with %s's title", f.From.Name, f.Provider.Name)
	return fillChapter{src: src, info: info, volume: fillVolume{info: volume, manga: fillManga{Manga: f.Manga, note: note}}}
}

type fillChapter struct {
	src    mangadata.Chapter
	info   mangadata.ChapterInfo
	volume fillVolume
}

func (c fillChapter) String() string              { return c.info.Title }
func (c fillChapter) Info() mangadata.ChapterInfo { return c.info }
func (c fillChapter) Volume() mangadata.Volume    { return c.volume }
func (c fillChapter) Unwrap() mangadata.Chapter   { return c.src }

type fillVolume struct {
	info  mangadata.VolumeInfo
	manga fillManga
}

func (v fillVolume) String() string             { return stringutil.FormatFloat32(v.info.Number) }
func (v fillVolume) Info() mangadata.VolumeInfo { return v.info }
func (v fillVolume) Manga() mangadata.Manga     { return v.manga }

// fillManga is the manga with its metadata noted (the ComicInfo.xml Notes).
type fillManga struct {
	mangadata.Manga
	note string
}

func (m fillManga) Metadata() metadata.Metadata {
	meta := m.Manga.Metadata()
	if metadata.Validate(meta) != nil {
		return meta
	}
	return notedMetadata{Metadata: meta, note: m.note}
}

func (m fillManga) SetMetadata(meta metadata.Metadata) {
	if n, ok := meta.(notedMetadata); ok {
		meta = n.Metadata
	}
	m.Manga.SetMetadata(meta)
}

type notedMetadata struct {
	metadata.Metadata
	note string
}

func (m notedMetadata) Notes() string {
	return strings.TrimSpace(m.note + "\n\n" + m.Metadata.Notes())
}

func (m notedMetadata) AgeRating() string {
	if r, ok := m.Metadata.(metadata.AgeRated); ok {
		return r.AgeRating()
	}
	return ""
}

// DownloadFill downloads the chapters (Fill.Chapter's) with the client, From's, as
// Download does (the provider's lock, rate-limit retries, events), into the fill's
// folder: with Manga's binding as their metadata, a new manga folder getting Manga's
// .libmangal.json.
func DownloadFill(ctx context.Context, client *libmangal.Client, fill Fill, chapters []mangadata.Chapter, options libmangal.DownloadOptions, onEvent func(Event)) Results {
	return downloadWith(ctx, client, chapters, options, onEvent, fill.download)
}

func (f Fill) download(ctx context.Context, client *libmangal.Client, chapter mangadata.Chapter, options libmangal.DownloadOptions) (*metadata.DownloadedChapter, error) {
	manga := chapter.Volume().Manga()
	if !bindingsOff(ctx) {
		bindings, err := cache.Bindings()
		if err != nil {
			return nil, err
		}
		loginBound(ctx, bindings, f.Provider.ID, manga.Info().ID)
		bound, err := applyBinding(ctx, bindings, f.Provider.ID, client, manga)
		if err != nil {
			return nil, err
		}
		if bound {
			options.SearchMetadata = false
		}
	}
	options.Directory, options.CreateProviderDir, options.CreateMangaDir = f.Folder, false, false
	down, err := client.DownloadChapterContext(ctx, chapter, options)
	if err != nil && options.Strict && metadata.Validate(manga.Metadata()) != nil {
		return nil, fmt.Errorf("no valid metadata, bind the manga (mangal bind, or A in the TUI) or set download.metadata.strict = false: %w", err)
	}
	if err != nil {
		return down, err
	}

	parent, dir := f.Folder, ""
	if f.MangaDir {
		parent, dir = filepath.Dir(f.Folder), filepath.Base(f.Folder)
		if err := f.ensureDirInfo(client.FS(), parent, dir, manga); err != nil {
			return nil, err
		}
	}
	home := f.Manga.Info()
	from := mangadata.Chapter(chapter)
	if w, ok := chapter.(mangadata.WrappedChapter); ok {
		from = w.Unwrap()
	}
	// the chapter is downloaded either way, a failed record is only logged
	if err := recordIn(parent, dir, storage.Source{Provider: f.Provider.ID, MangaID: home.ID, Title: home.Title}, client.Info().ID, from.Volume().Manga().Info(), chapter, options.Format, down); err != nil {
		log.Error("%s", err)
	}
	return down, nil
}

// ensureDirInfo writes the .libmangal.json of Manga a download of it would write, unless
// the folder has one.
func (f Fill) ensureDirInfo(fsys afero.Fs, parent, dir string, manga mangadata.Manga) error {
	folder := filepath.Join(parent, dir)
	if _, err := fsys.Stat(filepath.Join(folder, libmangal.FilenameMangaDirInfo)); !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	info := libmangal.NewMangaDirInfo(f.Provider, manga)
	if err := libmangal.WriteMangaDirInfo(fsys, folder, info, LibraryModes().File); err != nil {
		return fmt.Errorf("write %s of %q: %w", libmangal.FilenameMangaDirInfo, f.Manga, err)
	}
	library, err := cache.Library()
	if err != nil {
		return err
	}
	return library.RememberMangaDir(parent, dir, info)
}
