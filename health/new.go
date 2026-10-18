package health

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/progress"
	"github.com/luevano/mangoprovider"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/client/kitsu"
	"github.com/luevano/mangal/v5/client/myanimelist"
	"github.com/luevano/mangal/v5/client/selector"
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
	m, err := watch.Connect(ctx, nil)
	if err != nil {
		return Checker{}, err
	}
	if svc, ok := m.(*watch.Service); ok {
		svc.OnError = func(err error) { log.Warn("%s", err) }
	}
	return NewWith(m)
}

// NewWith is New with the watches of m (mangal serve passes its own service).
func NewWith(m watch.Manager) (Checker, error) {
	st, err := cache.Storage()
	if err != nil {
		return Checker{}, err
	}
	loaders, skipped, err := manager.Load()
	if err != nil {
		return Checker{}, err
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
		Config: configKeys,
		Statuses: func(ctx context.Context, mangas []storage.Manga, bindings []storage.Binding, fetch bool) error {
			return client.LibraryStatuses(st.Library()).Update(ctx, mangas, bindings, fetch)
		},
		Chapters: listChapters,
		Probe: func(ctx context.Context, provider string, m ProviderManga) Probe {
			return probeProvider(ctx, loaders, provider, m)
		},
		SearchAnilist:  searchAnilist,
		AnilistFromMAL: anilistFromMAL,
		IDCheck:        checkIDs,
		Backups: func(ctx context.Context, mangas []storage.Manga) ([]library.Backup, error) {
			finder, err := client.LibraryBackups()
			if err != nil {
				return nil, err
			}
			return finder.Find(ctx, mangas)
		},
		MyAnimeList:  config.Metadata.MyAnimeList.Enabled.Get(),
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
		Parallel:       config.Watch.ParallelProviders.Get(),
		Root:           path.Downloads(),
		ProviderDirs:   config.Download.Provider.CreateDir.Get(),
		Backoff:        config.Watch.Backoff.Get(),
		CoverageMin:    float64(config.Watch.CoverageMin.Get()) / 100,
		CoverageExempt: config.Watch.CoverageExempt.Get(),
	}, nil
}

// listChapters lists the chapters of the provider's mangas as a watch check would,
// holding the provider's lock (a download or a check of it elsewhere is waited for).
func listChapters(ctx context.Context, provider string, mangas []ProviderManga, wait func(string), done func(int, ProviderChapters, error)) error {
	src, err := watch.ClientSource(ctx, provider)
	if err != nil {
		return err
	}
	defer watch.CloseSource(src)
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
		manga, err := watch.FindManga(ctx, src, m.MangaID, m.Title)
		if err != nil {
			done(i, ProviderChapters{}, err)
			continue
		}
		chapters, err := src.Chapters(ctx, manga)
		if err != nil {
			done(i, ProviderChapters{}, err)
			continue
		}
		done(i, ChaptersOf(chapters, m.Have), nil)
	}
	return nil
}

// mangaPlus is MangaPlus' provider id.
const mangaPlus = "mango-mangaplus"

// mangaPlusPaid is true when a paid MangaPlus device secret is set (saved or a
// secret_file): asking with it risks locking the device out.
func mangaPlusPaid() bool {
	secret, err := cache.MangaPlusSecret()
	return err != nil || secret != "" || config.Providers.MangaPlus.SecretFile.Get() != ""
}

// probeProvider asks the provider for the manga by its id (a title search when it can't),
// what the provider reports meanwhile kept and passed on; the account comes from the
// accounts cache (no request).
func probeProvider(ctx context.Context, loaders []libmangal.ProviderLoader, provider string, m ProviderManga) Probe {
	var p Probe
	if provider == mangaPlus && mangaPlusPaid() {
		p.Skipped = "a paid MangaPlus secret is set (asking risks a device lockout)"
		return p
	}
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	outer := ctx
	var mu sync.Mutex
	ctx = progress.WithReporter(ctx, func(st progress.Status) {
		progress.Report(outer, st)
		if st.IsMinor() {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if line := st.String(); !slices.Contains(p.Statuses, line) {
			p.Statuses = append(p.Statuses, line)
		}
	})
	c, _, release, err := client.HoldByID(ctx, provider)
	if err != nil {
		p.Err = fmt.Errorf("load: %w", err)
		return p
	}
	defer release()
	if l, _, ok := libmangal.FindLoader(loaders, provider); ok {
		if a, ok := l.(interface{ HasAccount() bool }); ok && a.HasAccount() {
			p.Account = cachedAccount(c.Info().ID)
		}
	}
	// a cached answer says nothing about the site: mangoprovider caches the lookup,
	// luaprovider the http responses (cleared as a watch check does)
	if st, err := cache.Storage(); err == nil {
		_ = st.ForgetMangaByID(c.Info().ID, m.MangaID)
	}
	if l, _, ok := libmangal.FindLoader(loaders, provider); ok {
		if _, mango := l.(*mangoprovider.Loader); !mango {
			_ = cache.ClearChapterCache(c.Info().ID)
		}
	}
	start := time.Now()
	_, found, err := selector.ByID(ctx, client.Searcher(c), m.MangaID, m.Title)
	p.Took = time.Since(start)
	mu.Lock()
	defer mu.Unlock()
	switch {
	case err != nil:
		p.Err = err
	case !found:
		p.Err = fmt.Errorf("manga %q: %w", m.MangaID, watch.ErrMangaNotFound)
	}
	return p
}

// cachedAccount is the provider's account as the accounts screen last saw it.
func cachedAccount(provider string) string {
	a, checked, ok, err := client.CachedAccount(provider)
	switch {
	case err != nil:
		return "unknown (" + err.Error() + ")"
	case !ok:
		return "not checked lately (the accounts screen checks it)"
	}
	s := a.Status
	if a.User != "" {
		s += " (" + a.User + ")"
	}
	return s + ", checked " + checked.Format(time.DateOnly)
}

// searchAnilist is AniList's closest match to each query, batched.
func searchAnilist(ctx context.Context, queries []metadata.Query) ([]metadata.Match, []bool, error) {
	ani, err := anilist.Anilist()
	if err != nil {
		return nil, nil, err
	}
	return ani.FindClosestManyContext(ctx, queries)
}

// anilistFromMAL maps MyAnimeList ids to AniList ones (cached, batched).
func anilistFromMAL(ctx context.Context, ids []int) (map[int]int, error) {
	ani, err := anilist.Anilist()
	if err != nil {
		return nil, err
	}
	return ani.IDsFromContext(ctx, metadata.IDCodeMyAnimeList, ids)
}

// checkIDs checks the AniList and Kitsu ids of the adopted folders, as library scan
// --check does (without its suggestions: M1 has them).
func checkIDs(ctx context.Context, mangas []storage.Manga) ([]library.IDCheck, error) {
	ani, err := anilist.Anilist()
	if err != nil {
		return nil, err
	}
	kt, err := kitsu.Kitsu()
	if err != nil {
		return nil, err
	}
	checker := library.IDChecker{FS: afs.Afero,
		Providers: map[metadata.IDSource]library.IDProvider{metadata.IDSourceAnilist: ani, metadata.IDSourceKitsu: kt}}
	return checker.Check(ctx, mangas)
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

// configKeys are the stale keys of the loaded config file (A3).
func configKeys() ([]ConfigKey, error) {
	var keys []ConfigKey
	for _, key := range config.Unsupported() {
		keys = append(keys, ConfigKey{Key: key, Suggestion: config.Suggest(key)})
	}
	for _, key := range config.Retired() {
		keys = append(keys, ConfigKey{Key: key, Retired: true, Reason: config.RetiredReason(key)})
	}
	return keys, nil
}
