package cmd

import (
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/health"
)

func TestPrintHealth(t *testing.T) {
	report := health.Report{
		Findings: []health.Finding{
			{Check: "W1", Category: health.Watches, Severity: health.Error, Subject: health.Subject{Kind: "watch", ID: 4, Name: "Dorohedoro (mango-mangadex)"},
				Message: "needs re-link", Fix: health.Fix{Commands: []string{"mangal watch move 4"}, Note: "searches its title"}},
			{Check: "M6", Category: health.Metadata, Severity: health.Warning, Subject: health.Subject{Kind: "manga", ID: 1, Name: "One-Punch Man"},
				Message: "AniList 74347 is in 2 folders, keep 1", Details: []string{"keep 1  One-Punch Man"},
				Fix: health.Fix{Commands: []string{"mangal library merge 1 2"}}},
		},
		Counts: health.Counts{Error: 1, Warning: 1},
	}
	var b strings.Builder
	printHealth(&b, report)
	want := `watches: 1 (1 error)
  [error] W1 watch 4 Dorohedoro (mango-mangadex): needs re-link
      fix: mangal watch move 4
      (searches its title)

metadata: 1 (1 warning)
  [warning] M6 manga 1 One-Punch Man: AniList 74347 is in 2 folders, keep 1
      keep 1  One-Punch Man
      fix: mangal library merge 1 2

2 findings: 1 error, 1 warning
"mangal library bind/merge/move" and "mangal migrate" only list what they'd do, --apply does it
`
	if b.String() != want {
		t.Errorf("got\n%s\nwant\n%s", b.String(), want)
	}
	b.Reset()
	printHealth(&b, health.Report{})
	if b.String() != "no problems found\n" {
		t.Errorf("empty: %q", b.String())
	}
}
