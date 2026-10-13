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
			{Check: "W10", Category: health.Watches, Severity: health.Info, Subject: health.Subject{Kind: "library", Name: "library"},
				Message: "2 mangas aren't watched: 1 ongoing/unknown, 1 finished", Fix: health.Fix{Commands: []string{"mangal health --only W10"}},
				Items: []health.Finding{
					{Subject: health.Subject{Kind: "manga", ID: 3, Name: "Kingdom"}, Message: "ongoing, not watched", Fix: health.Fix{Commands: []string{"mangal watch add mango-mangadex mid:k"}}},
					{Subject: health.Subject{Kind: "manga", ID: 5, Name: "Death Note"}, Message: "finished, not watched (fine)"},
				}},
		},
		Counts: health.Counts{Error: 1, Warning: 1, Info: 1},
	}
	var b strings.Builder
	printHealth(&b, report, nil)
	want := `watches: 2 (1 error, 1 info)
  [error] W1 watch 4 Dorohedoro (mango-mangadex): needs re-link
      fix: mangal watch move 4
      (searches its title)
  [info] W10 library: 2 mangas aren't watched: 1 ongoing/unknown, 1 finished
      fix: mangal health --only W10

metadata: 1 (1 warning)
  [warning] M6 manga 1 One-Punch Man: AniList 74347 is in 2 folders, keep 1
      keep 1  One-Punch Man
      fix: mangal library merge 1 2

3 findings: 1 error, 1 warning, 1 info
"mangal library bind/merge/move/mark/rename", "library scan --mark" and "mangal migrate" only list what they'd do, --apply does it
`
	if b.String() != want {
		t.Errorf("got\n%s\nwant\n%s", b.String(), want)
	}
	// named in --only, its items are listed
	b.Reset()
	printHealth(&b, health.Report{Findings: report.Findings[2:], Counts: health.Counts{Info: 1}}, []string{"w10"})
	want = `watches: 1 (1 info)
  [info] W10 library: 2 mangas aren't watched: 1 ongoing/unknown, 1 finished
      fix: mangal health --only W10
      manga 3 Kingdom: ongoing, not watched
          fix: mangal watch add mango-mangadex mid:k
      manga 5 Death Note: finished, not watched (fine)

1 finding: 1 info
`
	if b.String() != want {
		t.Errorf("got\n%s\nwant\n%s", b.String(), want)
	}
	b.Reset()
	printHealth(&b, health.Report{}, nil)
	if b.String() != "no problems found\n" {
		t.Errorf("empty: %q", b.String())
	}
}
