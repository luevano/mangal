package config

import (
	"context"
	"maps"
	"slices"
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

// TestJobsPerProvider: the jobs' tables are made of each provider's settings, with the
// trait's default for a provider not set; a table write goes to the settings.
func TestJobsPerProvider(t *testing.T) {
	reset(t)
	m := useMem(t, map[string]string{"jobs.urls.providers": `{"mango-mangadex":"off"}`})
	plus := libmangal.ProviderInfo{ID: "mango-mangaplus", Traits: libmangal.ProviderTraits{AvoidUnattended: true}}
	dex := libmangal.ProviderInfo{ID: "mango-mangadex"}
	RegisterProviders([]libmangal.ProviderInfo{plus, dex})
	loaders := []libmangal.ProviderLoader{fakeLoader{plus}, fakeLoader{dex}}
	for key, want := range map[string]string{
		"providers.mango-mangaplus.job_sources": "off", "providers.mango-mangadex.job_sources": "on",
		"providers.mango-mangaplus.job_check": "on", "providers.mango-mangadex.job_urls": "off",
	} {
		if got, _ := Get(key); got != want {
			t.Errorf("%s = %v, want %s", key, got, want)
		}
		if ProviderOf(key) == "" || IsRisky(key) {
			t.Errorf("%s: not a provider's plain setting", key)
		}
	}
	if !IsAlias("jobs.urls.providers") || slices.Contains(ListedKeys(), "jobs.urls.providers") {
		t.Error("the table is listed")
	}

	// a provider's setting: the table has it, the old row is split first
	if err := WriteKeyBy("providers.mango-mangaplus.job_urls", "on", "test"); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"providers.mango-mangaplus.job_urls": `"on"`, "providers.mango-mangadex.job_urls": `"off"`}
	if !maps.Equal(m.values, want) {
		t.Errorf("stored %v", m.values)
	}
	if p := Jobs.URLs.Providers.Fresh(); !p.Unattended(loaders, plus.ID) || p.Unattended(loaders, dex.ID) {
		t.Errorf("table %v", p)
	}

	// the table: what it names is set, the rest back to the default
	if err := Jobs.URLs.Providers.Write(ProviderSettings{"mango-mangadex": "48h"}); err != nil {
		t.Fatal(err)
	}
	want = map[string]string{"providers.mango-mangadex.job_urls": `"48h"`}
	if !maps.Equal(m.values, want) {
		t.Errorf("stored %v", m.values)
	}
	if p := Jobs.URLs.Providers.Fresh(); p.Unattended(loaders, plus.ID) || p.Override(dex.ID) != "48h" {
		t.Errorf("table %v", p)
	}
	if err := WriteKeyBy("providers.mango-mangadex.job_health", "12h", "test"); err == nil {
		t.Error("an interval for the network health")
	}

	// a provider's variable is in the table too
	t.Setenv("MANGAL_PROVIDERS_MANGO_MANGADEX_JOB_SOURCES", "off")
	refreshAll(t)
	if p := Jobs.Sources.Providers.Get(); p.Unattended(loaders, dex.ID) {
		t.Errorf("the variable: %v", p)
	}

	set, drop := SplitAliases(map[string]string{"jobs.check.providers": `{"a":"off","b":"0 * * * *"}`, "providers.b.job_check": `"on"`, "download.format": `"CBZ"`})
	if !maps.Equal(set, map[string]string{"providers.a.job_check": `"off"`}) || !slices.Equal(drop, []string{"jobs.check.providers"}) {
		t.Errorf("split %v %v", set, drop)
	}
}

// refreshAll makes viper's config again (a test changed the environment).
func refreshAll(t *testing.T) {
	t.Helper()
	lmu.Lock()
	applyLocked()
	lmu.Unlock()
}
