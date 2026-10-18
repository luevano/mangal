package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/config"
)

// TestProvidersRmRefusesLocalWork: a provider that isn't a git repository stays.
func TestProvidersRmRefusesLocalWork(t *testing.T) {
	dir := t.TempDir()
	old := config.Providers.Path.Get()
	if err := config.Set(config.Providers.Path.Key, dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.Set(config.Providers.Path.Key, old) })
	if err := os.MkdirAll(filepath.Join(dir, "mine"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := providersRmCmd.RunE(providersRmCmd, []string{"mine"})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("got %v, want a refusal", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "mine")); err != nil {
		t.Errorf("removed: %v", err)
	}
}
