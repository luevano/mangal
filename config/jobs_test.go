package config

import (
	"context"
	"testing"

	"github.com/luevano/libmangal"
)

func TestProviderSettings(t *testing.T) {
	reset(t)
	plus := libmangal.ProviderInfo{ID: "mango-mangaplus", Traits: libmangal.ProviderTraits{AvoidUnattended: true}}
	loaders := []libmangal.ProviderLoader{fakeLoader{plus}, fakeLoader{libmangal.ProviderInfo{ID: "mango-mangadex"}}}
	p := Jobs.Sources.Providers.Get()
	if p.Unattended(loaders, plus.ID) || !p.Unattended(loaders, "mango-mangadex") {
		t.Errorf("defaults: %v", p)
	}
	if p := Jobs.Check.Providers.Get(); !p.On(plus.ID) {
		t.Errorf("watch checks: MangaPlus off by default: %v", p)
	}
	m := useMem(t, nil)
	// another process wrote it: Fresh reads it
	m.values["jobs.sources.providers"], m.values["jobs.urls.providers"], m.rev = `{"mango-mangadex":"336h"}`, `{"mango-mangaplus":"On"}`, 1
	p = Jobs.Sources.Providers.Fresh()
	if p.Unattended(loaders, plus.ID) || p.Override("mango-mangadex") != "336h" || p.Override(plus.ID) != "" {
		t.Errorf("fresh: %v", p)
	}
	if p := Jobs.URLs.Providers.Fresh(); !p.Unattended(loaders, plus.ID) {
		t.Errorf("turned on: %v", p)
	}
	if err := Set("jobs.sources.providers", map[string]any{"x": "soon"}); err == nil {
		t.Error("an interval that isn't one was taken")
	}
	if err := Set("jobs.check.providers", map[string]any{"x": "0 */8 * * *"}); err != nil {
		t.Error(err)
	}
}

type fakeLoader struct{ info libmangal.ProviderInfo }

func (f fakeLoader) String() string                                   { return f.info.ID }
func (f fakeLoader) Info() libmangal.ProviderInfo                     { return f.info }
func (f fakeLoader) Load(context.Context) (libmangal.Provider, error) { return nil, nil }
