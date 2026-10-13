package client

import (
	"context"
	"errors"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/client/kitsu"
	"github.com/luevano/mangal/v5/client/myanimelist"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
)

// ReadChapter opens the chapter's file at path with the default app, syncs the reading
// progress (read.history.anilist, .kitsu and .myanimelist) and records it in the read history
// (read.history.local).
// A chapter that opened is recorded even if the sync failed.
func ReadChapter(ctx context.Context, c *libmangal.Client, path string, chapter mangadata.Chapter) error {
	bindings, err := cache.Bindings()
	if err != nil {
		return err
	}
	// the bound sources are synced here, with their ids, once it opened
	bound, options, err := boundSyncs(bindings, c.Info().ID, chapter, readOptions(ctx))
	if err != nil {
		return err
	}
	err = c.ReadChapterContext(ctx, path, chapter, options)
	if openFailed(err) {
		return err
	}
	err = errors.Join(err, syncBound(ctx, c, chapter, bound))
	if !config.Read.History.Local.Get() {
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
	// the client id is saved with the logins, config.ReadOptions can't tell
	options.SaveMyAnimeList = options.SaveMyAnimeList && myanimelist.Configured()
	if options.SaveMyAnimeList && !loggedIn(ctx, myanimelist.EnsureLogin, myanimelist.MyAnimeList) {
		log.Info("not logged into MyAnimeList, its reading progress isn't synced")
		options.SaveMyAnimeList = false
	}
	return options
}

func loggedIn(ctx context.Context, ensure func(context.Context) error, provider func() (*metadata.ProviderWithCache, error)) bool {
	_ = ensure(ctx)
	p, err := provider()
	return err == nil && p.Authenticated()
}

// openFailed is a ReadChapterContext error from opening the file, not from the sync.
func openFailed(err error) bool {
	var openErr *libmangal.OpenError
	return errors.As(err, &openErr)
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
