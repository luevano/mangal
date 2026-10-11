package client

import (
	"context"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
	"golang.org/x/oauth2"
)

// accountsTTL is how long a provider's account is cached: plans and payment dates rarely change.
const accountsTTL = 12 * time.Hour

// AccountEntry is an account of the accounts view.
type AccountEntry struct {
	libmangal.Account
	// Provider is the provider id, empty for metadata providers (e.g. Anilist).
	Provider string
	// Checked is when the account was fetched, it may come from the cache.
	Checked time.Time
	// Err is why the account couldn't be fetched.
	Err error
}

type cachedAccount struct {
	Account libmangal.Account
	Checked time.Time
}

// Accounts lists the Anilist login and the accounts of the providers that have one,
// cached for accountsTTL unless refresh.
func Accounts(ctx context.Context, refresh bool) ([]AccountEntry, error) {
	entries := []AccountEntry{anilistAccount()}

	loaders, err := manager.Loaders()
	if err != nil {
		return nil, err
	}
	cacheDB, _, err := cache.DBs()
	if err != nil {
		return nil, err
	}
	accounts := storage.KV{DB: cacheDB, Table: storage.TableOther, Owner: "accounts", TTL: accountsTTL}
	for _, loader := range loaders {
		if l, ok := loader.(interface{ HasAccount() bool }); !ok || !l.HasAccount() {
			continue
		}
		id := loader.Info().ID
		var cached cachedAccount
		if found, err := accounts.Get(id, &cached); err == nil && found && !refresh {
			entries = append(entries, AccountEntry{Account: cached.Account, Provider: id, Checked: cached.Checked})
			continue
		}

		entry := AccountEntry{Account: libmangal.Account{Service: loader.Info().Name}, Provider: id, Checked: time.Now()}
		client, err := NewClient(ctx, loader)
		if err == nil {
			var found bool
			entry.Account, found, err = client.AccountContext(ctx)
			if err == nil && !found {
				entry.Account = libmangal.Account{Service: loader.Info().Name, Status: "no account"}
			}
		}
		if err != nil {
			entry.Account.Service, entry.Err = loader.Info().Name, err
		} else if err := accounts.Set(id, cachedAccount{Account: entry.Account, Checked: entry.Checked}); err != nil {
			entry.Err = err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// anilistAccount is the Anilist login, with when its token expires.
func anilistAccount() AccountEntry {
	ani := anilist.Anilist()
	entry := AccountEntry{Account: metadata.AccountOf(ani), Checked: time.Now()}
	if user := ani.User(); user != nil {
		var token oauth2.Token
		if found, err := cache.GetAnilistAuthData(user.Name(), &token); err == nil && found {
			entry.Expires = token.Expiry
		}
	}
	return entry
}
