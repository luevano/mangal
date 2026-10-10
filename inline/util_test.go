package inline

import "testing"

func TestGetSelectedChaptersEmpty(t *testing.T) {
	if _, err := getSelectedChapters(Args{ChapterSelector: "first"}, nil); err == nil {
		t.Error("no error without chapters")
	}
}
