package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/core"
)

type searchManga struct{ info mangadata.MangaInfo }

func (m searchManga) String() string                { return m.info.Title }
func (m searchManga) Info() mangadata.MangaInfo     { return m.info }
func (m searchManga) Metadata() metadata.Metadata   { return nil }
func (m searchManga) SetMetadata(metadata.Metadata) {}

func TestSearchCmd(t *testing.T) {
	c, err := core.New(context.Background(), core.Env{
		SearchProvider: func(_ context.Context, provider, query string) (libmangal.ProviderInfo, []mangadata.Manga, error) {
			if provider == "down" {
				return libmangal.ProviderInfo{}, nil, errors.New("unreachable")
			}
			return libmangal.ProviderInfo{ID: provider, Name: "Pill"}, []mangadata.Manga{searchManga{mangadata.MangaInfo{ID: "2085", Title: query, URL: "https://x/2085"}}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	old := newCore
	newCore = func() (*core.Core, error) { return c, nil }
	t.Cleanup(func() { newCore = old; searchArgs.Providers = nil })

	var out, errOut bytes.Buffer
	searchCmd.SetOut(&out)
	searchCmd.SetErr(&errOut)
	searchArgs.Providers = []string{"pill", "down"}
	if err := searchCmd.RunE(searchCmd, []string{"dark", "gathering"}); err != nil {
		t.Fatal(err)
	}
	if want := "Pill (pill): 1 results\n  2085\tdark gathering\thttps://x/2085\n"; out.String() != want {
		t.Errorf("out %q, want %q", out.String(), want)
	}
	if !strings.Contains(errOut.String(), "down: error: unreachable") {
		t.Errorf("err %q", errOut.String())
	}

	searchArgs.Providers = []string{"down"}
	if err := searchCmd.RunE(searchCmd, []string{"x"}); err == nil {
		t.Error("every provider failed: no error")
	}
}
