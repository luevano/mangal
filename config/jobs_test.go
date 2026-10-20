package config

import (
	"testing"
)

func TestProviderSettings(t *testing.T) {
	reset(t)
	p := Jobs.Sources.Providers.Get()
	if p.On("mango-mangaplus") || !p.On("mango-mangadex") {
		t.Errorf("defaults: %v", p)
	}
	if p := Jobs.Check.Providers.Get(); !p.On("mango-mangaplus") {
		t.Errorf("watch checks: MangaPlus off by default: %v", p)
	}
	writeConfig(t, "[jobs.sources.providers]\nmango-mangadex = '336h'\n\n[jobs.urls.providers]\nmango-mangaplus = 'On'\n")
	// another process wrote it: Fresh reads it, the defaults stay for the rest
	p = Jobs.Sources.Providers.Fresh()
	if p.On("mango-mangaplus") || p.Override("mango-mangadex") != "336h" || p.Override("mango-mangaplus") != "" {
		t.Errorf("fresh: %v", p)
	}
	if p := Jobs.URLs.Providers.Fresh(); !p.On("mango-mangaplus") {
		t.Errorf("turned on: %v", p)
	}
	if err := Set("jobs.sources.providers", map[string]any{"x": "soon"}); err == nil {
		t.Error("an interval that isn't one was taken")
	}
	if err := Set("jobs.check.providers", map[string]any{"x": "0 */8 * * *"}); err != nil {
		t.Error(err)
	}
}
