package health

import (
	"context"
	"fmt"
	"time"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/client/kitsu"
	"github.com/luevano/mangal/v5/client/myanimelist"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/notify"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/template"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/luevano/mangal/v5/util/cache"
)

// New is the Checker over mangal's databases and the installed providers, with the
// watches of the running mangal serve when there's one (else the local service).
func New(ctx context.Context) (Checker, error) {
	st, err := cache.Storage()
	if err != nil {
		return Checker{}, err
	}
	loaders, skipped, err := manager.Load()
	if err != nil {
		return Checker{}, err
	}
	m, err := watch.Connect(ctx, nil)
	if err != nil {
		return Checker{}, err
	}
	if svc, ok := m.(*watch.Service); ok {
		svc.OnError = func(err error) { log.Warn("%s", err) }
	}
	return Checker{
		Library: st.Library(),
		Watches: m,
		Loaders: loaders,
		OldIDs:  func() ([]storage.OldID, error) { return st.FindOldIDs(loaders) },
		Accounts: func() (Accounts, error) {
			return Accounts{
				Notices: cache.Notices(),
				Logins:  savedLogins(),
				Skipped: skipped,
				Webhook: notify.WebhookURL() != "",
				Notify:  config.Watch.Notify.Get(),
			}, nil
		},
		Statuses: func(ctx context.Context, mangas []storage.Manga, bindings []storage.Binding, fetch bool) error {
			return client.LibraryStatuses(st.Library()).Update(ctx, mangas, bindings, fetch)
		},
		NewChapters:  listNewChapters,
		Ignores:      storage.HealthIgnores{DB: st.Data}.List,
		FS:           afs.Afero,
		Mark:         library.MarkOptions{ProviderDirs: config.Download.Provider.CreateDir.Get(), ProviderName: template.Provider},
		ProviderName: template.Provider,
		MangaName:    template.Manga,
		Metadata: func(ctx context.Context, ids [][]metadata.ID, fetch bool) ([]metadata.Metadata, error) {
			if fetch {
				return anilist.MetadataByIDsBatch(ctx, ids)
			}
			return anilist.MetadataByIDsCached(ctx, ids)
		},
		Parallel:     config.Watch.ParallelProviders.Get(),
		Root:         path.Downloads(),
		ProviderDirs: config.Download.Provider.CreateDir.Get(),
		Backoff:      config.Watch.Backoff.Get(),
	}, nil
}

// listNewChapters lists the new chapters of the provider's mangas as a watch check would,
// holding the provider's lock (a download or a check of it elsewhere is waited for).
func listNewChapters(ctx context.Context, provider string, mangas []ProviderManga, wait func(string), done func(int, []float32, error)) error {
	src, err := watch.ClientSource(ctx, provider)
	if err != nil {
		return err
	}
	store, err := cache.Watches()
	if err != nil {
		return err
	}
	ctx, unlock, err := client.LockProvider(ctx, store, src.Info().ID, client.LockForWatch, func(b client.ProviderBusy) { wait(b.Message()) })
	if err != nil {
		return err
	}
	defer unlock()
	for i, m := range mangas {
		if err := ctx.Err(); err != nil {
			return err
		}
		numbers, err := watch.NewChapters(ctx, src, m.MangaID, m.Title, m.Have)
		done(i, numbers, err)
	}
	return nil
}

// expiresSoon is when a login that expires counts, as anilist.LoginNotice.
const expiresSoon = 7 * 24 * time.Hour

// savedLogins are the saved logins that expired or expire soon (read locally).
func savedLogins() []Login {
	var logins []Login
	add := func(service, user, command string, expires time.Time, expired, needed bool) {
		switch {
		case expired:
			logins = append(logins, Login{Service: service, Command: command, Needed: needed,
				Message: fmt.Sprintf("the login of %q expired on %s", user, expires.Format(time.DateOnly))})
		case !expires.IsZero() && time.Until(expires) < expiresSoon:
			logins = append(logins, Login{Service: service, Command: command, Needed: needed,
				Message: fmt.Sprintf("the login of %q expires on %s", user, expires.Format(time.DateOnly))})
		}
	}
	if l, ok, err := anilist.SavedLogin(); err == nil && ok {
		add("AniList", l.User, "mangal anilist login", l.Expires, l.Expired(), true)
	}
	read := config.ReadOptions()
	// kitsu and myanimelist refresh their tokens: only a rejected refresh is a problem
	if l, ok, err := kitsu.SavedLogin(); err == nil && ok && l.Expired() {
		add("Kitsu", l.User, "mangal kitsu", l.Expires, true, read.SaveKitsu)
	}
	if l, ok, err := myanimelist.SavedLogin(); err == nil && ok && l.Expired() {
		add("MyAnimeList", l.User, "mangal myanimelist login", l.Expires, true, read.SaveMyAnimeList)
	}
	return logins
}
