package info

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luevano/libmangal"
)

func TestNew(t *testing.T) {
	type args struct {
		r io.Reader
	}
	tests := []struct {
		name     string
		args     args
		wantInfo Info
		wantErr  bool
	}{
		{
			name: "bundle",
			args: args{
				r: strings.NewReader(`
type = "lua"
id = "some-id"
`),
			},
			wantInfo: Info{
				ProviderInfo: libmangal.ProviderInfo{
					ID: "some-id",
				},
				Type: TypeLua,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotInfo, err := New(tt.args.r)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotInfo, tt.wantInfo) {
				t.Errorf("New() gotInfo = %v, want %v", gotInfo, tt.wantInfo)
			}
		})
	}
}

func TestNewHistory(t *testing.T) {
	toml := `
type = "lua"
id = "weebcentral"
name = "Weeb Central"
version = "0.4.0"
website = "https://weebcentral.com/"

[[history]]
id = "mangasee"
name = "MangaSee"
website = "https://mangasee123.com/"
`
	info, err := New(strings.NewReader(toml))
	if err != nil {
		t.Fatal(err)
	}
	want := []libmangal.ProviderAlias{{ID: "mangasee", Name: "MangaSee", Website: "https://mangasee123.com/"}}
	if !reflect.DeepEqual(info.History, want) {
		t.Errorf("got %+v, want %+v", info.History, want)
	}
	if !info.Matches("mangasee") {
		t.Error("doesn't match its old id")
	}
}

func TestMarkdown(t *testing.T) {
	for _, website := range []string{"https://example.com/x", "example.com", "%zz", ""} {
		i := Info{ProviderInfo: libmangal.ProviderInfo{Name: "x", Version: "0.1.0", Website: website}}
		md, err := i.Markdown()
		if err != nil {
			t.Fatalf("%q: %s", website, err)
		}
		if website != "" && !strings.Contains(md, "]("+website+")") {
			t.Errorf("%q: %s", website, md)
		}
	}
}

// testdata/saturno holds copies of the saturno bundle's mangal.toml files.
func TestNewSaturno(t *testing.T) {
	tests := map[string]Info{
		"bundle": {ProviderInfo: libmangal.ProviderInfo{ID: "saturno"}, Type: TypeBundle},
		"mangadex": {ProviderInfo: libmangal.ProviderInfo{
			ID: "mangadex", Name: "Mangadex", Version: "0.3.1",
			Description: "Mangadex scraper", Website: "https://mangadex.org/",
		}, Type: TypeLua},
		"mangapill": {ProviderInfo: libmangal.ProviderInfo{
			ID: "mangapill", Name: "Mangapill", Version: "0.2.2",
			Description: "Mangapill scraper", Website: "https://mangapill.com/",
		}, Type: TypeLua},
		"weebcentral": {ProviderInfo: libmangal.ProviderInfo{
			ID: "weebcentral", Name: "Weeb Central", Version: "0.3.2",
			Description: "Weeb Central scraper", Website: "https://weebcentral.com/",
			History: []libmangal.ProviderAlias{{ID: "mangasee", Name: "MangaSee", Website: "https://mangasee123.com/"}},
		}, Type: TypeLua},
	}
	for name, want := range tests {
		f, err := os.Open(filepath.Join("testdata", "saturno", name+".toml"))
		if err != nil {
			t.Fatal(err)
		}
		got, err := New(f)
		f.Close()
		if err != nil {
			t.Fatalf("%s: %s", name, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %+v, want %+v", name, got, want)
		}
	}
}

func TestNewUnknownKeys(t *testing.T) {
	_, err := New(strings.NewReader("type = \"lua\"\nid = \"x\"\nfoo = 1\n[[history]]\nid = \"y\"\nbar = 2\n"))
	if err == nil || err.Error() != "unknown keys: foo, history.bar" {
		t.Errorf("got %v", err)
	}
}

func TestEncode(t *testing.T) {
	want := Info{ProviderInfo: libmangal.ProviderInfo{
		ID: "x", Name: "X", Version: "0.1.0", Website: "https://x.org/",
		Mirrors: []string{"https://x.net/"},
		History: []libmangal.ProviderAlias{{ID: "y"}},
	}, Type: TypeLua}
	var sb strings.Builder
	if err := want.Encode(&sb); err != nil {
		t.Fatal(err)
	}
	got, err := New(strings.NewReader(sb.String()))
	if err != nil {
		t.Fatalf("%s\n%s", err, sb.String())
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestNewRateLimits(t *testing.T) {
	toml := `
type = "lua"
id = "weebcentral"
rate_limits = [
	{ rate = "1/2s", match = "weebcentral.com", burst = 2 },
	{ rate = "5/s" },
]
`
	got, err := New(strings.NewReader(toml))
	if err != nil {
		t.Fatal(err)
	}
	limits, err := got.LuaRateLimits()
	if err != nil {
		t.Fatal(err)
	}
	if len(limits) != 2 || limits[0].Permits != 1 || limits[0].Period != 2*time.Second || limits[0].Match != "weebcentral.com" || limits[0].Burst != 2 ||
		limits[1].Permits != 5 || limits[1].Period != time.Second || limits[1].Match != "" {
		t.Errorf("limits %+v", limits)
	}

	var sb strings.Builder
	if err := got.Encode(&sb); err != nil {
		t.Fatal(err)
	}
	again, err := New(strings.NewReader(sb.String()))
	if err != nil || !reflect.DeepEqual(again.RateLimits, got.RateLimits) {
		t.Errorf("round trip %+v %v:\n%s", again.RateLimits, err, sb.String())
	}

	for _, bad := range []string{
		"type = \"lua\"\nid = \"x\"\nrate_limits = [{ rate = \"often\" }]\n",
		"type = \"lua\"\nid = \"x\"\nrate_limits = [{ rate = \"1/s\", burst = -1 }]\n",
		"type = \"lua\"\nid = \"x\"\nrate_limits = [{ rate = \"1/s\", match = \"/api\" }]\n",
		"type = \"lua\"\nid = \"x\"\nrate_limits = [{ rate = \"1/s\", typo = 1 }]\n",
		"type = \"bundle\"\nid = \"x\"\nrate_limits = [{ rate = \"1/s\" }]\n",
	} {
		if _, err := New(strings.NewReader(bad)); err == nil {
			t.Errorf("no error for\n%s", bad)
		}
	}
}
