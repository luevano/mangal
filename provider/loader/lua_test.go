package loader

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
)

func TestWithBundleID(t *testing.T) {
	info := libmangal.ProviderInfo{ID: "weebcentral", History: []libmangal.ProviderAlias{{ID: "mangasee", Name: "MangaSee"}}}
	got := withBundleID(info, "saturno")
	if got.ID != "saturno-weebcentral" || got.History[0].ID != "saturno-mangasee" {
		t.Errorf("got %+v", got)
	}
	if info.History[0].ID != "mangasee" {
		t.Error("changed the original history")
	}
	if !got.Matches("saturno-mangasee") {
		t.Error("doesn't match the old bundled id")
	}
}

func TestGetLoaderBundlesSkipsBroken(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const script = "function SearchMangas(q) return {} end"
	write("good/mangal.toml", "type = \"lua\"\nid = \"good\"\nname = \"Good\"\nversion = \"0.1.0\"\n")
	write("good/main.lua", script)
	write("nomain/mangal.toml", "type = \"lua\"\nid = \"nomain\"\n")
	write("badkey/mangal.toml", "type = \"lua\"\nid = \"badkey\"\nwhat = 1\n")
	write("badkey/main.lua", script)
	write("bundle/mangal.toml", "type = \"bundle\"\nid = \"bundle\"\n")
	write("bundle/inner/mangal.toml", "type = \"lua\"\nid = \"inner\"\nname = \"Inner\"\nversion = \"0.1.0\"\n")
	write("bundle/inner/main.lua", script)
	write("bundle/broken/mangal.toml", "type = \"lua\"\nid = \"broken\"\n")
	write("notprovider/readme.md", "")

	loaders, problems, err := getLoaderBundles("", dir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, l := range loaders {
		ids = append(ids, l.Info().ID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"bundle-inner", "good"}) {
		t.Errorf("loaders %v", ids)
	}
	if len(problems) != 3 {
		t.Fatalf("problems %v", problems)
	}
	for _, p := range problems {
		msg := p.Error()
		if !strings.Contains(msg, "nomain") && !strings.Contains(msg, "badkey") && !strings.Contains(msg, "broken") {
			t.Errorf("unexpected problem %q", msg)
		}
	}

	if _, _, err := getLoaderBundles("", filepath.Join(dir, "missing")); err == nil {
		t.Error("no error for a missing dir")
	}
}
