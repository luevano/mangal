package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/util/cache"
)

// TestConfigExportImport: the export loads back unchanged, an edit is planned then
// applied as an operation, a file key or an unknown one is refused; the import's change
// is undone from the history.
func TestConfigExportImport(t *testing.T) {
	ctx := context.Background()
	if _, err := cache.Settings(); err != nil {
		t.Fatal(err)
	}
	cfg := NewConfig()
	t.Cleanup(func() { _, _ = cfg.Set(ctx, "download.skip_if_exists", true) })
	data, err := cfg.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "skip_if_exists = true") || strings.Contains(string(data), "webhook_url") || strings.Contains(string(data), "[log]") {
		t.Fatalf("export:\n%s", data)
	}
	if plan, err := cfg.ImportPlan(ctx, data); err != nil || len(plan) != 0 {
		t.Fatalf("the export changes %+v, %v", plan, err)
	}
	edited := strings.Replace(string(data), "skip_if_exists = true", "skip_if_exists = false", 1)
	plan, err := cfg.ImportPlan(ctx, []byte(edited))
	if err != nil || len(plan) != 1 || plan[0].Key != "download.skip_if_exists" {
		t.Fatalf("plan %+v, %v", plan, err)
	}
	if config.Download.SkipIfExists.Get() != true {
		t.Fatal("the plan changed it")
	}
	if id, err := cfg.Import(ctx, plan); err != nil || id == 0 {
		t.Fatalf("import %d, %v", id, err)
	}
	if config.Download.SkipIfExists.Get() != false {
		t.Error("not imported")
	}
	for _, bad := range []string{"[log]\nlevel = 'debug'\n", "[download]\nnope = 1\n", "[download]\nskip_if_exists = 'maybe'\n"} {
		if _, err := cfg.ImportPlan(ctx, []byte(bad)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: %v", bad, err)
		}
	}

	changes, err := cfg.History(ctx, "download.skip_if_exists", 1)
	if err != nil || len(changes) != 1 || !strings.Contains(changes[0].Who, "(import)") {
		t.Fatalf("history %+v, %v", changes, err)
	}
	if e, err := cfg.Undo(ctx, changes[0].ID); err != nil || e.Value != true {
		t.Fatalf("undo %+v, %v", e, err)
	}
	if _, err := cfg.Undo(ctx, changes[0].ID); !errors.Is(err, ErrInvalid) {
		t.Errorf("the same undo again: %v", err)
	}
}

// TestConfigImportOldProviderTable: an exported file of before the per-provider settings,
// with a jobs.*.providers table, is imported into each provider's setting.
func TestConfigImportOldProviderTable(t *testing.T) {
	ctx := context.Background()
	if _, err := cache.Settings(); err != nil {
		t.Fatal(err)
	}
	cfg := NewConfig()
	plan, err := cfg.ImportPlan(ctx, []byte("[jobs.sources.providers]\nfake = 'off'\n"))
	if err != nil || len(plan) != 1 || plan[0].Key != "jobs.sources.providers" {
		t.Fatalf("plan %+v, %v", plan, err)
	}
	if _, err := cfg.Import(ctx, plan); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = cfg.Set(ctx, "jobs.sources.providers", map[string]any{}) })
	if on := config.Jobs.Sources.Providers.Fresh().On; on("fake") {
		t.Error("fake still on for the source checks")
	}
	s, _ := cache.Settings()
	values, _, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := values["providers.fake.job_sources"]; !ok {
		t.Errorf("no providers.fake.job_sources row: %v", values)
	}
}
