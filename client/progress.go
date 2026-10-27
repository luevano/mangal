package client

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/client/kitsu"
	"github.com/luevano/mangal/v5/client/myanimelist"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
)

// SyncProgress sets the reading progress of a chapter the mangal user read without opening
// it here (the web reader) on the metadata sources the config syncs (read.history.anilist,
// .kitsu, .myanimelist) where the user is logged in (their own accounts; the local user's
// are this process' logins): with the manga's binding of the source, else its id in ids.
// It returns the sources synced; a source without an id is skipped.
func SyncProgress(ctx context.Context, user string, bindings []storage.Binding, ids map[metadata.IDCode]string, chapter float32) ([]string, error) {
	local, err := cache.LocalUser()
	if err != nil {
		return nil, err
	}
	if user != local {
		return syncAccounts(ctx, user, bindings, ids, chapter)
	}
	options := readOptions(ctx)
	enabled := map[metadata.IDCode]bool{
		metadata.IDCodeAnilist:     options.SaveAnilist,
		metadata.IDCodeKitsu:       options.SaveKitsu,
		metadata.IDCodeMyAnimeList: options.SaveMyAnimeList,
	}
	providers := map[metadata.IDCode]func() (*metadata.ProviderWithCache, error){
		metadata.IDCodeAnilist:     anilist.Anilist,
		metadata.IDCodeKitsu:       kitsu.Kitsu,
		metadata.IDCodeMyAnimeList: myanimelist.MyAnimeList,
	}
	progress := int(math.Trunc(float64(chapter)))
	var synced []string
	var errs []error
	for _, src := range MetadataSources {
		if !enabled[src.Code] || providers[src.Code] == nil {
			continue
		}
		id := ids[src.Code]
		for _, b := range bindings {
			if b.Source == src.Code {
				id = b.ID
				break
			}
		}
		if id == "" {
			log.Info("no %s id, its reading progress isn't synced", src.Name)
			continue
		}
		err := func() error {
			n, err := strconv.Atoi(id)
			if err != nil {
				return fmt.Errorf("id %q isn't an integer", id)
			}
			p, err := providers[src.Code]()
			if err != nil {
				return err
			}
			return p.SetMangaProgressContext(ctx, n, progress)
		}()
		if err != nil {
			log.Warn("set the progress on %s (%s): %s", src.Name, id, err)
			errs = append(errs, fmt.Errorf("set progress on %s: %w", src.Name, err))
			continue
		}
		synced = append(synced, src.Name)
	}
	return synced, errors.Join(errs...)
}

// syncAccounts is SyncProgress for a user other than the local one: with their accounts
// (cache.AuthAccount), each an instance of its own (ForAccount).
func syncAccounts(ctx context.Context, user string, bindings []storage.Binding, ids map[metadata.IDCode]string, chapter float32) ([]string, error) {
	enabled := map[metadata.IDCode]bool{
		metadata.IDCodeAnilist:     config.Read.History.Anilist.Get(),
		metadata.IDCodeKitsu:       config.Read.History.Kitsu.Get(),
		metadata.IDCodeMyAnimeList: config.Read.History.MyAnimeList.Get(),
	}
	sources := map[metadata.IDCode]struct {
		key string
		get func(context.Context, string) (*metadata.ProviderWithCache, error)
	}{
		metadata.IDCodeAnilist:     {cache.AnilistAuthHistory, anilist.ForAccount},
		metadata.IDCodeKitsu:       {cache.KitsuAuthHistory, kitsu.ForAccount},
		metadata.IDCodeMyAnimeList: {cache.MyAnimeListAuthHistory, myanimelist.ForAccount},
	}
	progress := int(math.Trunc(float64(chapter)))
	var synced []string
	var errs []error
	for _, src := range MetadataSources {
		s, ok := sources[src.Code]
		if !ok || !enabled[src.Code] {
			continue
		}
		account, err := cache.AuthAccount(user, s.key)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if account == "" {
			continue
		}
		id := ids[src.Code]
		for _, b := range bindings {
			if b.Source == src.Code {
				id = b.ID
				break
			}
		}
		if id == "" {
			log.Info("no %s id, %s's reading progress isn't synced", src.Name, user)
			continue
		}
		err = func() error {
			n, err := strconv.Atoi(id)
			if err != nil {
				return fmt.Errorf("id %q isn't an integer", id)
			}
			p, err := s.get(ctx, account)
			if err != nil {
				return err
			}
			return p.SetMangaProgressContext(ctx, n, progress)
		}()
		if err != nil {
			log.Warn("set %s's progress on %s (%s): %s", user, src.Name, id, err)
			errs = append(errs, fmt.Errorf("set progress on %s: %w", src.Name, err))
			continue
		}
		synced = append(synced, src.Name)
	}
	return synced, errors.Join(errs...)
}
