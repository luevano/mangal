package cmd

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/spf13/cobra"
)

func findCmd(t *testing.T, name string) *cobra.Command {
	t.Helper()
	for _, c := range jobsCmd.Commands() {
		if c.Name() == name {
			return c
		}
	}
	t.Fatalf("no jobs %s", name)
	return nil
}

// jobs run scan, jobs (the list and a kind), set, pause and resume.
func TestJobsCmd(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dl")
	touchFile(t, filepath.Join(root, "Berserk", "Chapter 1.cbz"))
	old := config.Download.Path.Get()
	if err := config.Download.Path.Set(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.Download.Path.Set(old) })
	// the library is the package's: the folder scanned goes again
	t.Cleanup(func() {
		lib, err := cache.Library()
		if err != nil {
			t.Fatal(err)
		}
		m, ok, err := lib.MangaByFolder(root, "Berserk")
		if err != nil || !ok {
			t.Fatalf("scanned folder: %t %v", ok, err)
		}
		c, err := newCore()
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		p, err := c.Changes.Plan(ctx, core.ForgetReq{Mangas: []int64{m.ID}})
		if err == nil {
			_, err = c.Changes.ApplyPlan(ctx, p, core.ApplyOpts{})
		}
		if err != nil {
			t.Fatal(err)
		}
	})

	out, err := runCmd(t, jobsRunCmd, func() error { return jobsRunCmd.RunE(jobsRunCmd, []string{"scan"}) })
	if err != nil || !strings.Contains(out, "library scan: 1 chapters added") {
		t.Fatalf("run scan: %v\n%s", err, out)
	}
	out, err = runCmd(t, jobsCmd, func() error { return jobsCmd.RunE(jobsCmd, nil) })
	if err != nil || !strings.Contains(out, "scan     manual") || !strings.Contains(out, "1 chapters added") || !strings.Contains(out, "urls") {
		t.Errorf("jobs: %v\n%s", err, out)
	}
	if _, err := runCmd(t, jobsRunCmd, func() error { return jobsRunCmd.RunE(jobsRunCmd, []string{"download"}) }); err == nil {
		t.Error("download is no kind")
	}

	t.Cleanup(func() {
		_ = jobsSetCmd.Flags().Set("provider", "mango-mangadex=")
		_ = jobsSetCmd.Flags().Set("delay", "1s-3s")
		_ = jobsSetCmd.RunE(jobsSetCmd, []string{"urls"})
	})
	if err := jobsSetCmd.Flags().Set("provider", "mango-mangadex=off"); err != nil {
		t.Fatal(err)
	}
	if err := jobsSetCmd.Flags().Set("delay", "2s-4s"); err != nil {
		t.Fatal(err)
	}
	out, err = runCmd(t, jobsSetCmd, func() error { return jobsSetCmd.RunE(jobsSetCmd, []string{"urls"}) })
	if err != nil || !strings.Contains(out, "mango-mangadex") || !strings.Contains(out, "off (default off)") {
		t.Errorf("set: %v\n%s", err, out)
	}
	if config.Jobs.URLs.Providers.Get().On("mango-mangadex") || config.Jobs.DelayMax.Get() != "4s" {
		t.Errorf("not set: %v %s", config.Jobs.URLs.Providers.Get(), config.Jobs.DelayMax.Get())
	}
	jobsSetArgs.Providers = nil

	pause := findCmd(t, "pause")
	out, err = runCmd(t, pause, func() error { return pause.RunE(pause, []string{"urls"}) })
	if err != nil || !strings.Contains(out, "Source addresses: paused, next paused") {
		t.Errorf("pause: %v\n%s", err, out)
	}
	resume := findCmd(t, "resume")
	if out, err = runCmd(t, resume, func() error { return resume.RunE(resume, []string{"urls"}) }); err != nil || !strings.Contains(out, "Source addresses: on") {
		t.Errorf("resume: %v\n%s", err, out)
	}
}
