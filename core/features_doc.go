package core

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

//go:generate go run ./internal/featuresgen -docs

// Markers of the mode matrix in a markdown file (FeaturesMarkdown goes between them).
const (
	FeaturesBegin = "<!-- features:begin (go generate ./core) -->"
	FeaturesEnd   = "<!-- features:end -->"
)

// FeaturesMarkdown is the feature × mode matrix of Features as a markdown table: each
// mode's commands, key, function, operations or routes, "–" where it doesn't have it
// (the reasons in the last column).
func FeaturesMarkdown() string {
	var b strings.Builder
	b.WriteString("| Feature | CLI | TUI | Script | API | Web | Change | Not in, why |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|\n")
	code := func(list []string) string {
		if len(list) == 0 {
			return "–"
		}
		quoted := make([]string, len(list))
		for i, s := range list {
			quoted[i] = "`" + s + "`"
		}
		return strings.Join(quoted, ", ")
	}
	one := func(s string) []string {
		if s == "" {
			return nil
		}
		return []string{s}
	}
	for _, f := range Features {
		var why []string
		for _, m := range Modes {
			if r := f.NotApplicable[m]; r != "" {
				why = append(why, fmt.Sprintf("%s: %s", m, r))
			}
		}
		cells := []string{f.Name, code(f.CLI), code(one(f.TUI)), code(one(f.Script)), code(f.API), code(f.Web), code(one(string(f.Change))), strings.Join(why, "; ")}
		for i, c := range cells {
			cells[i] = strings.ReplaceAll(c, "|", `\|`)
		}
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	fmt.Fprintf(&b, "\nCommands of no feature (the modes' own): %s.\n", code(slices.Sorted(maps.Keys(ModeCommands))))
	return b.String()
}

// ReplaceFeatures is doc with the matrix between the markers, false when it has none.
func ReplaceFeatures(doc string) (string, bool) {
	begin := strings.Index(doc, FeaturesBegin)
	end := strings.Index(doc, FeaturesEnd)
	if begin < 0 || end < begin {
		return doc, false
	}
	return doc[:begin+len(FeaturesBegin)] + "\n" + FeaturesMarkdown() + doc[end:], true
}
