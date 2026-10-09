package config

import (
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
)

func TestPathEntryExpandsHome(t *testing.T) {
	if err := Library.Path.Set("~/manga"); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(xdg.Home, "manga")
	if got := Library.Path.Get(); got != want {
		t.Errorf("wanted %q, got %q", want, got)
	}
}
