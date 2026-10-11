package client

import (
	"context"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/client/kitsu"
	"github.com/luevano/mangal/v5/client/myanimelist"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/provider/loader"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
)

// accountsTTL is how long a provider's account is cached: plans and payment dates rarely change.
const accountsTTL = 12 * time.Hour

// AccountEntry is an account of the accounts view.
type AccountEntry struct {
	libmangal.Account
	// Provider is the provider id, empty for metadata providers (e.g. Anilist).
	Provider string
	// Metadata is the metadata provider's source, zero for providers.
	Metadata metadata.IDSource
	// Checked is when the account was fetched, it may come from the cache.
	Checked time.Time
	// Err is why the account couldn't be fetched.
	Err error
	// Discord is the Discord webhook row (notifications).
	Discord bool
}

type cachedAccount struct {
	Account libmangal.Account
	Checked time.Time
}

// Accounts lists every account mangal can use, logged in or not: the Anilist, Kitsu and
// MyAnimeList logins and the accounts of the providers that have one, cached for accountsTTL
// unless refresh. A provider that can't be loaded is a row with the error.
func Accounts(ctx context.Context, refresh bool) ([]AccountEntry, error) {
	entries := metadataAccounts(ctx, refresh)

	accounts, err := cache.AccountsStore(accountsTTL)
	if err != nil {
		return nil, err
	}
	loaders, err := loader.MangoLoaders()
	if err != nil {
		entries = append(entries, AccountEntry{Account: libmangal.Account{Service: "Providers"}, Checked: time.Now(), Err: err})
	}
	lua, problems, err := loader.LuaLoaders()
	for _, p := range problems {
		log.Warn("skipped %s", p)
	}
	if err != nil {
		entries = append(entries, AccountEntry{Account: libmangal.Account{Service: "Lua providers"}, Checked: time.Now(), Err: err})
	}
	for _, l := range append(loaders, lua...) {
		if a, ok := l.(interface{ HasAccount() bool }); ok && a.HasAccount() {
			entries = append(entries, providerAccount(ctx, accounts, l, refresh))
		}
	}
	return append(entries, DiscordAccount()), nil
}

// DiscordAccount is the Discord webhook of the notifications: saved (mangal notify webhook),
// from the config or not set.
func DiscordAccount() AccountEntry {
	entry := AccountEntry{Account: libmangal.Account{Service: "Discord", Status: "not set"}, Discord: true, Checked: time.Now()}
	saved, err := cache.DiscordWebhook()
	switch {
	case err != nil:
		entry.Err = err
	case saved != "":
		entry.Status = "webhook set"
	case config.Notification.Discord.WebhookURL.Get() != "":
		entry.Status = "webhook set"
		entry.Details = append(entry.Details, "from "+config.Notification.Discord.WebhookURL.Key)
	default:
		entry.Details = append(entry.Details, "l sets the webhook")
	}
	return entry
}

// metadataAccounts are the metadata providers' logins, by priority.
func metadataAccounts(ctx context.Context, refresh bool) []AccountEntry {
	return []AccountEntry{anilistAccount(ctx, refresh), kitsuAccount(ctx, refresh), myAnimeListAccount(ctx, refresh)}
}

func providerAccount(ctx context.Context, accounts storage.KV, pl libmangal.ProviderLoader, refresh bool) AccountEntry {
	id, name := pl.Info().ID, pl.Info().Name
	var cached cachedAccount
	if found, err := accounts.Get(id, &cached); err == nil && found && !refresh {
		return AccountEntry{Account: cached.Account, Provider: id, Checked: cached.Checked}
	}

	entry := AccountEntry{Account: libmangal.Account{Service: name}, Provider: id, Checked: time.Now()}
	client, err := Acquire(ctx, pl)
	if err == nil {
		var found bool
		entry.Account, found, err = client.AccountContext(ctx)
		if err == nil && !found {
			entry.Account = libmangal.Account{Service: name, Status: "no account"}
		}
	}
	if err != nil {
		entry.Account.Service, entry.Err = name, err
	} else if err := accounts.Set(id, cachedAccount{Account: entry.Account, Checked: entry.Checked}); err != nil {
		entry.Err = err
	}
	return entry
}

// anilistAccount is the Anilist login, with when its token expires; refresh tries the
// saved login again if it couldn't be checked.
func anilistAccount(ctx context.Context, refresh bool) AccountEntry {
	ani, err := anilist.Anilist()
	if err != nil {
		return AccountEntry{Account: libmangal.Account{Service: "Anilist"}, Metadata: metadata.IDSourceAnilist, Checked: time.Now(), Err: err}
	}
	if refresh {
		_ = anilist.CheckLogin(ctx)
	} else {
		_ = anilist.EnsureLogin(ctx)
	}
	entry := AccountEntry{Account: metadata.AccountOf(ani), Metadata: metadata.IDSourceAnilist, Checked: time.Now()}
	login, found, err := anilist.SavedLogin()
	switch {
	case err != nil:
		entry.Err = err
	case !found:
	case ani.Authenticated():
		entry.Expires = login.Expires
	case login.Expired():
		entry.User, entry.Status, entry.Expires = login.User, "expired", login.Expires
		entry.Details = append(entry.Details, "l logs in again")
	case login.Err != nil:
		entry.User, entry.Status, entry.Expires, entry.Err = login.User, "not checked", login.Expires, login.Err
	}
	return entry
}

// savedLogin is a metadata provider's saved login, read locally.
type savedLogin struct {
	user    string
	expired bool
	// err is why the login couldn't be checked this run
	err error
}

// refreshedLogin is a metadata provider whose token is refreshed when it expires (so no
// expiry is shown), and that can be disabled in the config.
type refreshedLogin struct {
	service string
	source  metadata.IDSource
	enabled bool
	// enabledKey is the config key that disables it
	enabledKey    string
	provider      func() (*metadata.ProviderWithCache, error)
	ensure, check func(context.Context) error
	saved         func() (savedLogin, bool, error)
}

// account is the login's entry: a disabled provider shows its saved user without logging
// in; refresh tries the saved login again if it couldn't be checked.
func (r refreshedLogin) account(ctx context.Context, refresh bool) AccountEntry {
	p, err := r.provider()
	if err != nil {
		return AccountEntry{Account: libmangal.Account{Service: r.service}, Metadata: r.source, Checked: time.Now(), Err: err}
	}
	switch {
	case !r.enabled:
	case refresh:
		_ = r.check(ctx)
	default:
		_ = r.ensure(ctx)
	}
	entry := AccountEntry{Account: metadata.AccountOf(p), Metadata: r.source, Checked: time.Now()}
	login, found, err := r.saved()
	switch {
	case err != nil:
		entry.Err = err
	case !found, p.Authenticated():
	case login.expired:
		entry.User, entry.Status = login.user, "expired"
		entry.Details = append(entry.Details, "l logs in again")
	case !r.enabled:
		entry.User = login.user
	case login.err != nil:
		entry.User, entry.Status, entry.Err = login.user, "not checked", login.err
	}
	if !r.enabled {
		entry.Status = "disabled"
		entry.Details = append(entry.Details, r.enabledKey+" is off")
	}
	return entry
}

// kitsuAccount is the Kitsu login.
func kitsuAccount(ctx context.Context, refresh bool) AccountEntry {
	return refreshedLogin{
		service:    "Kitsu",
		source:     metadata.IDSourceKitsu,
		enabled:    config.Metadata.Kitsu.Enabled.Get(),
		enabledKey: config.Metadata.Kitsu.Enabled.Key,
		provider:   kitsu.Kitsu,
		ensure:     kitsu.EnsureLogin,
		check:      kitsu.CheckLogin,
		saved: func() (savedLogin, bool, error) {
			l, found, err := kitsu.SavedLogin()
			return savedLogin{user: l.User, expired: l.Expired(), err: l.Err}, found, err
		},
	}.account(ctx, refresh)
}

// myAnimeListAccount is the MyAnimeList login, "not configured" without a client id.
func myAnimeListAccount(ctx context.Context, refresh bool) AccountEntry {
	if config.Metadata.MyAnimeList.ClientID.Get() == "" {
		return AccountEntry{
			Account: libmangal.Account{
				Service: "MyAnimeList",
				URL:     "https://myanimelist.net/",
				Status:  "not configured",
				Details: []string{"set " + config.Metadata.MyAnimeList.ClientID.Key},
			},
			Metadata: metadata.IDSourceMyAnimeList,
			Checked:  time.Now(),
		}
	}
	return refreshedLogin{
		service:    "MyAnimeList",
		source:     metadata.IDSourceMyAnimeList,
		enabled:    config.Metadata.MyAnimeList.Enabled.Get(),
		enabledKey: config.Metadata.MyAnimeList.Enabled.Key,
		provider:   myanimelist.MyAnimeList,
		ensure:     myanimelist.EnsureLogin,
		check:      myanimelist.CheckLogin,
		saved: func() (savedLogin, bool, error) {
			l, found, err := myanimelist.SavedLogin()
			return savedLogin{user: l.User, expired: l.Expired(), err: l.Err}, found, err
		},
	}.account(ctx, refresh)
}
