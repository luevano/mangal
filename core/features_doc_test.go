package core

import (
	"strings"
	"testing"
)

// every feature is a row, the markers' content is replaced
func TestFeaturesMarkdown(t *testing.T) {
	md := FeaturesMarkdown()
	rows := 0
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "| ") && !strings.HasPrefix(line, "| Feature ") {
			rows++
		}
	}
	if rows != len(Features) {
		t.Errorf("%d rows, %d features", rows, len(Features))
	}
	doc := "intro\n" + FeaturesBegin + "\nthe previous matrix\n" + FeaturesEnd + "\nrest\n"
	got, ok := ReplaceFeatures(doc)
	if !ok || strings.Contains(got, "the previous matrix") || !strings.HasPrefix(got, "intro\n"+FeaturesBegin+"\n| Feature") || !strings.HasSuffix(got, FeaturesEnd+"\nrest\n") {
		t.Errorf("replaced:\n%s", got)
	}
	if _, ok := ReplaceFeatures("no markers"); ok {
		t.Error("no markers replaced")
	}
}
