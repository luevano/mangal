package client

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/myanimelist"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/util/cache"
)

// isolate keeps the logins and caches away from the user's databases.
func isolate(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MANGAL_CACHE_PATH", filepath.Join(dir, "cache"))
	t.Setenv("MANGAL_AUTH_KEYRING", "false")
	t.Setenv("MANGAL_PROVIDERS_PATH", filepath.Join(dir, "providers"))
	dataHome := xdg.DataHome
	xdg.DataHome = filepath.Join(dir, "data")
	t.Cleanup(func() {
		xdg.DataHome = dataHome
		_ = config.Metadata.Kitsu.Enabled.Set(true)
		_ = config.Metadata.MyAnimeList.Enabled.Set(true)
		_ = cache.SetMyAnimeListClient("", "")
	})
}

func entryOf(t *testing.T, entries []AccountEntry, service string) AccountEntry {
	t.Helper()
	for _, e := range entries {
		if e.Service == service {
			return e
		}
	}
	t.Fatalf("no %s row in %+v", service, entries)
	return AccountEntry{}
}

func TestMetadataAccountsAlwaysListed(t *testing.T) {
	isolate(t)
	ctx := context.Background()

	entries := metadataAccounts(ctx, false)
	if len(entries) != 3 {
		t.Fatalf("rows %+v", entries)
	}
	for _, service := range []string{"Anilist", "Kitsu"} {
		if e := entryOf(t, entries, service); e.Status != "not logged in" || e.Err != nil {
			t.Errorf("%s: %+v", service, e)
		}
	}
	mal := entryOf(t, entries, "MyAnimeList")
	if mal.Status != "not configured" || mal.Metadata != metadata.IDSourceMyAnimeList || !strings.Contains(strings.Join(mal.Details, " "), "client id") {
		t.Errorf("unconfigured myanimelist: %+v", mal)
	}

	if err := cache.SetMyAnimeListClient("some-id", ""); err != nil {
		t.Fatal(err)
	}
	if e := entryOf(t, metadataAccounts(ctx, false), "MyAnimeList"); e.Status != "not logged in" || e.Err != nil {
		t.Errorf("configured myanimelist: %+v", e)
	}
	if !myanimelist.Configured() {
		t.Error("not configured with a saved client id")
	}

	_ = config.Metadata.MyAnimeList.Enabled.Set(false)
	_ = config.Metadata.Kitsu.Enabled.Set(false)
	if myanimelist.Configured() {
		t.Error("configured while disabled")
	}
	entries = metadataAccounts(ctx, false)
	for _, service := range []string{"Kitsu", "MyAnimeList"} {
		if e := entryOf(t, entries, service); e.Status != "disabled" || len(e.Details) == 0 {
			t.Errorf("disabled %s: %+v", service, e)
		}
	}
}

func TestAccountsListsProviders(t *testing.T) {
	isolate(t)
	// MangaPlus' account from the cache, no request
	accounts, err := cache.AccountsStore(accountsTTL)
	if err != nil {
		t.Fatal(err)
	}
	cached := cachedAccount{Account: libmangal.Account{Service: "MangaPlus", Status: "free device"}, Checked: time.Now()}
	if err := accounts.Set("mango-mangaplus", cached); err != nil {
		t.Fatal(err)
	}

	entries, err := Accounts(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, service := range []string{"Anilist", "Kitsu", "MyAnimeList"} {
		entryOf(t, entries, service)
	}
	if e := entryOf(t, entries, "MangaPlus"); e.Status != "free device" || e.Provider != "mango-mangaplus" {
		t.Errorf("mangaplus: %+v", e)
	}
	if e := entryOf(t, entries, "Discord"); e.Status != "not set" || !e.Discord {
		t.Errorf("discord: %+v", e)
	}
}

func TestDiscordAccount(t *testing.T) {
	isolate(t)
	defer func() { _ = config.Notification.Discord.WebhookURL.Set("") }()
	if err := config.Notification.Discord.WebhookURL.Set("https://discord.com/api/webhooks/1/config"); err != nil {
		t.Fatal(err)
	}
	if e := DiscordAccount(); e.Status != "webhook set" || len(e.Details) != 1 {
		t.Errorf("from the config: %+v", e)
	}
	if err := cache.SetDiscordWebhook("https://discord.com/api/webhooks/1/saved"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cache.SetDiscordWebhook("") }()
	if e := DiscordAccount(); e.Status != "webhook set" || len(e.Details) != 0 {
		t.Errorf("saved: %+v", e)
	}
}

func TestMetadataProvidersMyAnimeListLast(t *testing.T) {
	isolate(t)
	if err := cache.SetMyAnimeListClient("some-id", ""); err != nil {
		t.Fatal(err)
	}
	metas, err := metadataProviders()
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 3 || metas[2].Info().Source != metadata.IDSourceMyAnimeList {
		t.Errorf("providers %v, want myanimelist last", metas)
	}
	_ = config.Metadata.MyAnimeList.Enabled.Set(false)
	if metas, _ := metadataProviders(); len(metas) != 2 {
		t.Errorf("providers with myanimelist disabled %v", metas)
	}
}
