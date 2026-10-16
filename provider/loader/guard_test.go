package loader

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/util/netguard"
)

func TestProviderClientsGuarded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	get := func(client *http.Client) error {
		resp, err := client.Get(server.URL)
		if err == nil {
			resp.Body.Close()
		}
		return err
	}
	set := func(allow bool, mirrors map[string]string) {
		t.Helper()
		if err := config.Providers.Private.Allow.Set(allow); err != nil {
			t.Fatal(err)
		}
		if err := config.Providers.Mirrors.Set(mirrors); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { set(false, map[string]string{}) })
	t.Setenv("MANGAL_PROVIDERS_MANGAPLUS_SECRET_FILE", "")

	set(false, map[string]string{})
	clients := map[string]func() *http.Client{
		"mango": func() *http.Client { return mangoOptions().HTTPClient },
		"lua":   func() *http.Client { return LuaOptions(t.TempDir()).HTTPClient },
	}
	for name, client := range clients {
		if err := get(client()); !errors.Is(err, netguard.ErrRefused) {
			t.Errorf("%s: got %v, want refused", name, err)
		}
	}

	set(false, map[string]string{"mango-test": server.URL + "/"})
	for name, client := range clients {
		if err := get(client()); err != nil {
			t.Errorf("%s with the server as a mirror: %v", name, err)
		}
	}

	set(true, map[string]string{})
	for name, client := range clients {
		if err := get(client()); err != nil {
			t.Errorf("%s with providers.allow_private_addresses: %v", name, err)
		}
	}
}
