package client

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
)

// MetadataSource is a metadata source mangas can be bound to (storage.Bindings).
type MetadataSource struct {
	Code   metadata.IDCode
	Source metadata.IDSource
	// Name is for display, Key for flags and arguments (anilist, kitsu, mal).
	Name, Key string
	// URL is the page of a manga of the source.
	URL string
}

// MetadataSources are the sources a manga can be bound to, in their order of priority (the
// first bound one is the default, see storage.Bindings).
var MetadataSources = []MetadataSource{
	{Code: metadata.IDCodeAnilist, Source: metadata.IDSourceAnilist, Name: "AniList", Key: "anilist", URL: "https://anilist.co/manga/"},
	{Code: metadata.IDCodeKitsu, Source: metadata.IDSourceKitsu, Name: "Kitsu", Key: "kitsu", URL: "https://kitsu.app/manga/"},
	{Code: metadata.IDCodeMyAnimeList, Source: metadata.IDSourceMyAnimeList, Name: "MyAnimeList", Key: "mal", URL: "https://myanimelist.net/manga/"},
}

// MetadataSourceOf is the source with the code.
func MetadataSourceOf(code metadata.IDCode) (MetadataSource, bool) {
	for _, s := range MetadataSources {
		if s.Code == code {
			return s, true
		}
	}
	return MetadataSource{}, false
}

// ParseMetadataSource is a source by key, code or name (anilist/al, kitsu/kt,
// mal/myanimelist), any case.
func ParseMetadataSource(s string) (MetadataSource, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, src := range MetadataSources {
		if s == src.Key || s == string(src.Code) || s == strings.ToLower(src.Name) {
			return src, nil
		}
	}
	return MetadataSource{}, fmt.Errorf("unknown metadata source %q (anilist, kitsu or mal)", s)
}

// SourceName is the name of the source with the code, the code if unknown.
func SourceName(code metadata.IDCode) string {
	if s, ok := MetadataSourceOf(code); ok {
		return s.Name
	}
	return string(code)
}

// metadataProvider is a libmangal.Client's metadata providers.
type metadataProvider interface {
	GetMetadataProvider(id metadata.IDSource) (*metadata.ProviderWithCache, error)
}

// BoundMetadata is the metadata of the source's manga id, an error if there's no such
// manga or the client has no provider for the source (MyAnimeList without a client id).
func BoundMetadata(ctx context.Context, c metadataProvider, code metadata.IDCode, id string) (metadata.Metadata, error) {
	ensureLogin(ctx, code)
	return boundMetadata(ctx, c, code, id)
}

// ensureLogin logs into AniList for its sources: a logged in user sees adult mangas too.
func ensureLogin(ctx context.Context, code metadata.IDCode) {
	if code == metadata.IDCodeAnilist {
		_ = anilist.EnsureLogin(ctx)
	}
}

func boundMetadata(ctx context.Context, c metadataProvider, code metadata.IDCode, id string) (metadata.Metadata, error) {
	src, ok := MetadataSourceOf(code)
	if !ok {
		return nil, fmt.Errorf("unknown metadata source %q", code)
	}
	n, err := strconv.Atoi(id)
	if err != nil || n <= 0 {
		return nil, fmt.Errorf("%s id %q isn't a positive integer", src.Name, id)
	}
	p, err := c.GetMetadataProvider(src.Source)
	if err != nil {
		return nil, fmt.Errorf("%s isn't available (not enabled or configured): %w", src.Name, err)
	}
	meta, found, err := p.SearchByIDContext(ctx, n)
	if err != nil {
		return nil, fmt.Errorf("get %s manga %d: %w", src.Name, n, err)
	}
	if !found {
		return nil, fmt.Errorf("%s manga %d not found", src.Name, n)
	}
	return meta, nil
}

type withoutBindingKey struct{}

// WithoutBinding makes the downloads with the ctx keep the manga's metadata (e.g. inline's
// --anilist-id or the provider's metadata), not the default binding's.
func WithoutBinding(ctx context.Context) context.Context {
	return context.WithValue(ctx, withoutBindingKey{}, true)
}

func bindingsOff(ctx context.Context) bool {
	off, _ := ctx.Value(withoutBindingKey{}).(bool)
	return off
}

// ApplyBinding sets the metadata of the manga's default binding (if any) as its metadata,
// false when it has none. When the default can't be fetched (not found, any error but a
// rate limit) the next bound source by priority is used, logged. Downloads do it (see
// WithoutBinding), so libmangal doesn't search.
func ApplyBinding(ctx context.Context, c *libmangal.Client, manga mangadata.Manga) (bool, error) {
	bindings, err := cache.Bindings()
	if err != nil {
		return false, err
	}
	loginBound(ctx, bindings, c.Info().ID, manga.Info().ID)
	return applyBinding(ctx, bindings, c.Info().ID, c, manga)
}

// loginBound logs into the sources the manga is bound to that need it (see ensureLogin).
func loginBound(ctx context.Context, bindings storage.Bindings, provider, mangaID string) {
	if list, err := bindings.Get(provider, mangaID); err == nil {
		for _, b := range list {
			ensureLogin(ctx, b.Source)
		}
	}
}

func applyBinding(ctx context.Context, bindings storage.Bindings, provider string, c metadataProvider, manga mangadata.Manga) (bool, error) {
	list, err := bindings.Get(provider, manga.Info().ID)
	if err != nil || len(list) == 0 {
		return false, err
	}
	if meta := manga.Metadata(); meta != nil {
		for _, b := range list {
			if meta.ID().Code == b.Source && meta.ID().Raw == b.ID {
				// a backup stays while the ones before it keep failing
				return true, nil
			}
			if !recentlyFailed(b) {
				break
			}
		}
	}
	meta, err := firstBound(ctx, c, list, manga.String())
	if err != nil {
		return false, fmt.Errorf("bound metadata of %q: %w", manga, err)
	}
	manga.SetMetadata(meta)
	return true, nil
}

// failedTTL is how long a binding that couldn't be fetched is skipped for the backup the
// manga already has, so each chapter of a download doesn't ask again.
const failedTTL = 10 * time.Minute

var failed = struct {
	sync.Mutex
	at map[string]time.Time
}{at: map[string]time.Time{}}

func failedKey(b storage.Binding) string { return string(b.Source) + ":" + b.ID }

func recentlyFailed(b storage.Binding) bool {
	failed.Lock()
	defer failed.Unlock()
	at, ok := failed.at[failedKey(b)]
	return ok && time.Since(at) < failedTTL
}

func setFailed(b storage.Binding, failedNow bool) {
	failed.Lock()
	defer failed.Unlock()
	if failedNow {
		failed.at[failedKey(b)] = time.Now()
	} else {
		delete(failed.at, failedKey(b))
	}
}

// firstBound is the metadata of the first of the bindings (by priority) that can be
// fetched. A rate limit or a cancelled ctx stops it (the download waits and tries again).
func firstBound(ctx context.Context, c metadataProvider, bindings []storage.Binding, manga string) (metadata.Metadata, error) {
	var errs []error
	for i, b := range bindings {
		meta, err := boundMetadata(ctx, c, b.Source, b.ID)
		if err == nil {
			setFailed(b, false)
			if i > 0 {
				log.Warn("manga %q: using its %s binding %s instead", manga, SourceName(b.Source), b.ID)
			}
			return meta, nil
		}
		if _, limited := RetryAfter(err); limited || ctx.Err() != nil {
			return nil, err
		}
		setFailed(b, true)
		errs = append(errs, err)
		if i < len(bindings)-1 {
			log.Warn("manga %q: %s, falling back to %s", manga, err, SourceName(bindings[i+1].Source))
		}
	}
	return nil, errors.Join(errs...)
}

// SearchMetadata is the metadata of the manga's default binding (or the next that can be
// fetched, see ApplyBinding), else libmangal's search (title bindings, the closest match of
// each metadata provider).
func SearchMetadata(ctx context.Context, c *libmangal.Client, manga mangadata.Manga) (metadata.Metadata, error) {
	bindings, err := cache.Bindings()
	if err != nil {
		return nil, err
	}
	list, err := bindings.Get(c.Info().ID, manga.Info().ID)
	if err != nil {
		return nil, err
	}
	if len(list) > 0 {
		loginBound(ctx, bindings, c.Info().ID, manga.Info().ID)
		return firstBound(ctx, c, list, manga.String())
	}
	return c.SearchMetadataContext(ctx, manga)
}

// Bind binds the provider's manga to the source's manga id after getting its metadata
// (returned). When it's the default (the first bound source by priority) and manga isn't
// nil, the metadata is set on it.
func Bind(ctx context.Context, c *libmangal.Client, provider, mangaID string, manga mangadata.Manga, code metadata.IDCode, id string) (metadata.Metadata, error) {
	bindings, err := cache.Bindings()
	if err != nil {
		return nil, err
	}
	ensureLogin(ctx, code)
	return bind(ctx, bindings, c, provider, mangaID, manga, code, id)
}

func bind(ctx context.Context, bindings storage.Bindings, c metadataProvider, provider, mangaID string, manga mangadata.Manga, code metadata.IDCode, id string) (metadata.Metadata, error) {
	meta, err := boundMetadata(ctx, c, code, id)
	if err != nil {
		return nil, err
	}
	if err := bindings.Set(storage.Binding{Provider: provider, MangaID: mangaID, Source: code, ID: id}); err != nil {
		return nil, err
	}
	log.Info("bound %s %q to %s %s (%s)", provider, mangaID, SourceName(code), id, meta)
	if manga != nil {
		if d, found, err := bindings.Default(provider, mangaID); err == nil && found && d.Source == code {
			manga.SetMetadata(meta)
		}
	}
	return meta, nil
}

// boundSyncs are the manga's bindings of the sources options syncs, and the options
// without them (libmangal syncs the rest, finding the id from the manga's metadata).
func boundSyncs(bindings storage.Bindings, provider string, chapter mangadata.Chapter, options libmangal.ReadOptions) ([]storage.Binding, libmangal.ReadOptions, error) {
	all, err := bindings.Get(provider, chapter.Volume().Manga().Info().ID)
	if err != nil {
		return nil, options, err
	}
	enabled := map[metadata.IDCode]*bool{
		metadata.IDCodeAnilist:     &options.SaveAnilist,
		metadata.IDCodeKitsu:       &options.SaveKitsu,
		metadata.IDCodeMyAnimeList: &options.SaveMyAnimeList,
	}
	var bound []storage.Binding
	for _, b := range all {
		if on := enabled[b.Source]; on != nil && *on {
			*on = false
			bound = append(bound, b)
		}
	}
	return bound, options, nil
}

// syncBound sets the reading progress of the chapter on each binding's source, with its id.
func syncBound(ctx context.Context, c metadataProvider, chapter mangadata.Chapter, bound []storage.Binding) error {
	progress := int(math.Trunc(float64(chapter.Info().Number)))
	var errs []error
	for _, b := range bound {
		src, _ := MetadataSourceOf(b.Source)
		err := func() error {
			id, err := strconv.Atoi(b.ID)
			if err != nil {
				return fmt.Errorf("id %q isn't an integer", b.ID)
			}
			p, err := c.GetMetadataProvider(src.Source)
			if err != nil {
				return err
			}
			return p.SetMangaProgressContext(ctx, id, progress)
		}()
		if err != nil {
			log.Warn("set the progress on %s (bound %s): %s", src.Name, b.ID, err)
			errs = append(errs, fmt.Errorf("set progress on %s: %w", src.Name, err))
		}
	}
	return errors.Join(errs...)
}
