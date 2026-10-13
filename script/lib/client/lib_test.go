package client

import (
	"context"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/logger"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	lua "github.com/yuin/gopher-lua"
)

type manga struct{ info mangadata.MangaInfo }

func (m *manga) String() string                { return m.info.Title }
func (m *manga) Info() mangadata.MangaInfo     { return m.info }
func (m *manga) Metadata() metadata.Metadata   { return nil }
func (m *manga) SetMetadata(metadata.Metadata) {}
func (v *volume) String() string               { return "vol" }
func (v *volume) Info() mangadata.VolumeInfo   { return mangadata.VolumeInfo{Number: 1} }
func (v *volume) Manga() mangadata.Manga       { return v.manga }
func (c *chapter) String() string              { return c.info.Title }
func (c *chapter) Info() mangadata.ChapterInfo { return c.info }
func (c *chapter) Volume() mangadata.Volume    { return c.volume }

type volume struct{ manga *manga }

type chapter struct {
	info   mangadata.ChapterInfo
	volume *volume
}

// provider has one manga with one volume of two chapters.
type provider struct{ libmangal.Provider }

var info = libmangal.ProviderInfo{ID: "fake", Name: "Fake", Version: "0.1.0"}

func (provider) String() string               { return "fake" }
func (provider) Close() error                 { return nil }
func (provider) Info() libmangal.ProviderInfo { return info }
func (provider) SetLogger(*logger.Logger)     {}
func (provider) SearchMangas(_ context.Context, query string) ([]mangadata.Manga, error) {
	return []mangadata.Manga{&manga{mangadata.MangaInfo{ID: "m1", Title: "Found " + query}}}, nil
}

func (provider) MangaVolumes(_ context.Context, m mangadata.Manga) ([]mangadata.Volume, error) {
	return []mangadata.Volume{&volume{m.(*manga)}}, nil
}

func (provider) VolumeChapters(_ context.Context, v mangadata.Volume) ([]mangadata.Chapter, error) {
	vol := v.(*volume)
	return []mangadata.Chapter{
		&chapter{mangadata.ChapterInfo{Title: "One", Number: 1}, vol},
		&chapter{mangadata.ChapterInfo{Title: "Two", Number: 2}, vol},
	}, nil
}

type loader struct{}

func (loader) String() string               { return "fake" }
func (loader) Info() libmangal.ProviderInfo { return info }
func (loader) Load(context.Context) (libmangal.Provider, error) {
	return provider{}, nil
}

func run(t *testing.T, client *libmangal.Client, script string) (lua.LValue, error) {
	t.Helper()
	state := lua.NewState()
	t.Cleanup(state.Close)
	state.PreloadModule(libName, Lib(client).Loader())
	if err := state.DoString(script); err != nil {
		return nil, err
	}
	return state.Get(-1), nil
}

func TestClient(t *testing.T) {
	c, err := libmangal.NewClientContext(context.Background(), loader{}, libmangal.DefaultClientOptions())
	if err != nil {
		t.Fatal(err)
	}
	got, err := run(t, c, `
local client = require("client")
local mangas = client.search_mangas("x")
local volumes = client.manga_volumes(mangas[1])
local chapters = client.volume_chapters(volumes[1])
local out = {}
for _, ch in ipairs(chapters) do
	out[#out + 1] = ch:info().title .. "=" .. ch:info().number
end
return mangas[1]:info().title .. " " .. mangas[1]:info().id .. " vol " .. volumes[1]:info().number .. ": " .. table.concat(out, ",")`)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Found x m1 vol 1: One=1,Two=2"; got.String() != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNoClient(t *testing.T) {
	_, err := run(t, nil, `return require("client").search_mangas("x")`)
	if err == nil || !strings.Contains(err.Error(), "missing provider client") {
		t.Errorf("got %v", err)
	}
}
