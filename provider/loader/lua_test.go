package loader

import (
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
