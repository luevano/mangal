package jobs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/luevano/mangal/v5/client/jobs"
)

// A job its screen announced has no added notice, the others do.
func TestSilencedNotice(t *testing.T) {
	m := jobs.New(context.Background())
	t.Cleanup(func() { m.Shutdown(time.Second) })
	a := &activity{m: m}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	spec := jobs.Spec{Kind: jobs.KindDownload, Title: "Berserk: 2 chapters", Providers: []string{"p"}}

	quietMu.Lock()
	j := m.Add(spec)
	quiet[j.ID] = true
	quietMu.Unlock()
	for {
		msg := a.Wait(ctx).(ChangedMsg)
		if n := msg.Notice(); n != "" && !strings.HasPrefix(n, "Downloaded") {
			t.Errorf("notice %q of a silenced job", n)
		}
		if len(msg.Finished) > 0 {
			break
		}
	}
	if silenced(j.ID) {
		t.Error("the silenced id stayed")
	}

	m.Add(spec)
	if n := a.Wait(ctx).(ChangedMsg).Notice(); n == "" {
		t.Error("no notice of an added job")
	}
}
