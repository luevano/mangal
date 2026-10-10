package providers

import (
	"testing"

	"github.com/luevano/libmangal"
)

func TestFormerly(t *testing.T) {
	info := libmangal.ProviderInfo{ID: "mango-mangasee", Name: "Weeb Central", History: []libmangal.ProviderAlias{
		{ID: "mango-mangasee", Name: "MangaSee"},
		{ID: "mango-old"},
		{ID: "mango-older", Name: "MangaSee"},
	}}
	if got, want := formerly(info), "MangaSee, mango-old"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
