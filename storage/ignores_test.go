package storage

import (
	"path/filepath"
	"testing"
)

func TestHealthIgnores(t *testing.T) {
	data, err := OpenData(filepath.Join(t.TempDir(), FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	h := HealthIgnores{DB: data}
	if err := h.Dismiss(HealthIgnore{Check: "M3", Subject: "manga/5/al", Message: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := h.Dismiss(HealthIgnore{Check: "M3", Subject: "manga/5/al", Message: "y"}); err != nil {
		t.Fatal(err)
	}
	list, err := h.List()
	if err != nil || len(list) != 1 || list[0].Message != "y" || list[0].Dismissed.IsZero() {
		t.Fatalf("list %+v, %v", list, err)
	}
	if ok, err := h.Undismiss("M3", "manga/5/al"); !ok || err != nil {
		t.Errorf("undismiss: %v, %v", ok, err)
	}
	if ok, _ := h.Undismiss("M3", "manga/5/al"); ok {
		t.Error("undismissed twice")
	}
}
