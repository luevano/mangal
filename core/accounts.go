package core

import (
	"context"
	"fmt"
	"time"

	"github.com/luevano/libmangal/metadata"
	lmanilist "github.com/luevano/libmangal/metadata/anilist"
	"github.com/luevano/mangal/v5/auth/oauth"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/client/myanimelist"
)

// Accounts are the accounts mangal can use, logged in or not: the metadata logins and the
// providers' accounts. Logging in stays with the UIs (it's interactive).
type Accounts interface {
	List(ctx context.Context, opts AccountsOptions) ([]Account, error)
	// APIClients are the AniList and MyAnimeList API clients the logins go through.
	APIClients(ctx context.Context) ([]APIClient, error)
	// SetAPIClient saves the tracker's (anilist, myanimelist) API client, an empty id
	// removes it; a nil secret keeps the saved one when the id stays.
	SetAPIClient(ctx context.Context, source, id string, secret *string) error
}

// APIClient is a tracker's API client, registered by whoever runs mangal (none ships with
// it). Its secret is never read back.
type APIClient struct {
	// Source is anilist or myanimelist, Name its name.
	Source, Name string
	// ID is the client id, "" when none is saved.
	ID string
	// Secret is true when a client secret is saved.
	Secret bool
	// Pin is AniList's metadata.anilist.redirect = "pin".
	Pin bool
	// RedirectURL is what the client must be registered with, RegisterURL where.
	RedirectURL, RegisterURL string
}

// AccountsOptions are how the accounts are looked up.
type AccountsOptions struct {
	// Refresh asks every site again; Cached asks none (the providers' accounts as last
	// checked). Neither asks the sites whose answer is older than 12 hours.
	Refresh, Cached bool `json:",omitempty"`
}

// Account is an account's status.
type Account struct {
	metadata.Account
	// Provider is the provider's id, "" for a metadata login.
	Provider string `json:"provider,omitempty"`
	// Metadata is the metadata login's source, zero for a provider.
	Metadata metadata.IDSource `json:"metadata,omitempty"`
	// Checked is when it was looked up (maybe from the cache).
	Checked time.Time `json:"checked,omitzero"`
	// Error is why it couldn't be looked up.
	Error string `json:"error,omitempty"`
	// Discord is the Discord webhook's row (notifications).
	Discord bool `json:"discord,omitempty"`
	// Secret is true when a secret can be saved for the provider's account.
	Secret bool `json:"secret,omitempty"`
}

// NewAccounts are mangal's accounts, without the rest of the services.
func NewAccounts() Accounts { return localAccounts{} }

type localAccounts struct{}

func (localAccounts) List(ctx context.Context, opts AccountsOptions) ([]Account, error) {
	var entries []client.AccountEntry
	var err error
	if opts.Cached {
		entries, err = client.AccountsCached(ctx)
	} else {
		entries, err = client.Accounts(ctx, opts.Refresh)
	}
	if err != nil {
		return nil, err
	}
	accounts := make([]Account, len(entries))
	for i, e := range entries {
		accounts[i] = AccountOf(e)
	}
	return accounts, nil
}

func (localAccounts) APIClients(context.Context) ([]APIClient, error) {
	alID, alSecret, err := anilist.Client()
	if err != nil {
		return nil, err
	}
	malID, malSecret, err := myanimelist.Client()
	if err != nil {
		return nil, err
	}
	redirect := oauth.AnilistServerCallbackURL
	if anilist.Pin() {
		redirect = lmanilist.OAuthPinURL
	}
	return []APIClient{
		{Source: "anilist", Name: "AniList", ID: alID, Secret: alSecret != "", Pin: anilist.Pin(), RedirectURL: redirect,
			RegisterURL: "https://anilist.co/settings/developer"},
		{Source: "myanimelist", Name: "MyAnimeList", ID: malID, Secret: malSecret != "", RedirectURL: oauth.MyAnimeListServerCallbackURL,
			RegisterURL: "https://myanimelist.net/apiconfig"},
	}, nil
}

func (localAccounts) SetAPIClient(_ context.Context, source, id string, secret *string) error {
	get, set := anilist.Client, anilist.SetClient
	switch source {
	case "anilist":
	case "myanimelist":
		get, set = myanimelist.Client, myanimelist.SetClient
	default:
		return fmt.Errorf("%w: no API client of %q", ErrInvalid, source)
	}
	if secret == nil {
		savedID, saved, err := get()
		if err != nil {
			return err
		}
		if savedID != id {
			saved = ""
		}
		secret = &saved
	}
	return set(id, *secret)
}

// AccountOf is the entry's account.
func AccountOf(e client.AccountEntry) Account {
	a := Account{Account: e.Account, Provider: e.Provider, Metadata: e.Metadata, Checked: e.Checked, Discord: e.Discord, Secret: e.Secret}
	if e.Err != nil {
		a.Error = e.Err.Error()
	}
	return a
}
