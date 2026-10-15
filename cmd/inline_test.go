package cmd

import "testing"

func TestInlineNoMetadataFlag(t *testing.T) {
	f := inlineJSONCmd.Flags()
	if f.Lookup("no-metadata") == nil {
		t.Fatal("no --no-metadata")
	}
	if old := f.Lookup("anilist-disable"); old == nil || !old.Hidden {
		t.Errorf("--anilist-disable should stay as a hidden alias: %+v", old)
	}
	for _, name := range []string{"no-metadata", "anilist-disable"} {
		inlineArgs.AnilistDisable = false
		if err := f.Set(name, "true"); err != nil {
			t.Fatal(err)
		}
		if !inlineArgs.AnilistDisable {
			t.Errorf("--%s doesn't disable the metadata search", name)
		}
		_ = f.Set(name, "false")
	}
}
