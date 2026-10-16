package core

import (
	"context"
	"time"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
)

// Accounts are the accounts mangal can use, logged in or not: the metadata logins and the
// providers' accounts. Logging in stays with the UIs (it's interactive).
type Accounts interface {
	List(ctx context.Context, opts AccountsOptions) ([]Account, error)
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

// AccountOf is the entry's account.
func AccountOf(e client.AccountEntry) Account {
	a := Account{Account: e.Account, Provider: e.Provider, Metadata: e.Metadata, Checked: e.Checked, Discord: e.Discord}
	if e.Err != nil {
		a.Error = e.Err.Error()
	}
	return a
}
