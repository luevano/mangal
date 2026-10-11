package client

import (
	"context"
	"strings"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/client/kitsu"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
)

// ReadChapter opens the chapter's file at path with the default app, syncs the reading
// progress (read.history.anilist and .kitsu) and records it in the read history
// (read.history.local).
// A chapter that opened is recorded even if the sync failed.
func ReadChapter(ctx context.Context, c *libmangal.Client, path string, chapter mangadata.Chapter) error {
	err := c.ReadChapterContext(ctx, path, chapter, readOptions(ctx))
	if openFailed(err, path) || !config.Read.History.Local.Get() {
		return err
	}
	// the chapter was opened either way, a failed record is only logged
	if rerr := recordRead(c.Info(), chapter, config.Download.Provider.CreateDir.Get(), time.Now()); rerr != nil {
		log.Error("%s", rerr)
	}
	return err
}

// readOptions are the configured ones, without the progress syncs of the metadata providers
// that aren't logged in (not an error: syncing is on by default, a login isn't).
func readOptions(ctx context.Context) libmangal.ReadOptions {
	options := config.ReadOptions()
	if options.SaveAnilist && !loggedIn(ctx, anilist.EnsureLogin, anilist.Anilist) {
		log.Info("not logged into Anilist, its reading progress isn't synced")
		options.SaveAnilist = false
	}
	if options.SaveKitsu && !loggedIn(ctx, kitsu.EnsureLogin, kitsu.Kitsu) {
		log.Info("not logged into Kitsu, its reading progress isn't synced")
		options.SaveKitsu = false
	}
	return options
}

func loggedIn(ctx context.Context, ensure func(context.Context) error, provider func() (*metadata.ProviderWithCache, error)) bool {
	_ = ensure(ctx)
	p, err := provider()
	return err == nil && p.Authenticated()
}

// openFailed is a ReadChapterContext error from opening the file, not from the sync.
// TODO: a typed error from libmangal instead of its message.
func openFailed(err error, path string) bool {
	return err != nil && strings.HasPrefix(err.Error(), "open "+path+": ")
}

func recordRead(provider libmangal.ProviderInfo, chapter mangadata.Chapter, providerDirs bool, at time.Time) error {
	lib, err := cache.Library()
	if err != nil {
		return err
	}
	history, err := cache.History()
	if err != nil {
		return err
	}
	return record(history, lib, provider, chapter, providerDirs, at)
}

func record(history storage.History, lib storage.Library, provider libmangal.ProviderInfo, chapter mangadata.Chapter, providerDirs bool, at time.Time) error {
	manga := chapter.Volume().Manga()
	r := storage.Read{
		Provider:     provider.ID,
		MangaID:      manga.Info().ID,
		MangaTitle:   manga.Info().Title,
		Volume:       chapter.Volume().Info().Number,
		Chapter:      chapter.Info().Number,
		ChapterTitle: chapter.Info().Title,
		ChapterURL:   chapter.Info().URL,
		Last:         at,
	}
	// not in the library is fine, the read is still recorded
	if m, ok, err := lib.MangaFor(provider, manga, providerDirs); err != nil {
		log.Error("library manga of %q: %s", manga, err)
	} else if ok {
		r.LibraryManga = m.ID
	}
	return history.Record(r)
}
