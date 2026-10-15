package core

import (
	"bytes"
	"os"
	"testing"
)

// the web UI's features.json is up to date (go generate ./core writes it)
func TestWebFeaturesJSON(t *testing.T) {
	want, err := WebFeaturesJSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../web/ui/src/features.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("web/ui/src/features.json is stale: go generate ./core")
	}
}

// every feature with a change kind or a web route has it in the API too: the web UI
// only talks to the API
func TestWebFeaturesHaveAPI(t *testing.T) {
	for _, f := range Features {
		if f.Has(ModeWeb) && !f.Has(ModeAPI) {
			t.Errorf("%s: in the web UI, not in the API", f.Name)
		}
		if f.Has(ModeAPI) && !f.Has(ModeWeb) && f.NotApplicable[ModeWeb] == "" {
			t.Errorf("%s: in the API, not in the web UI and no reason", f.Name)
		}
	}
}
