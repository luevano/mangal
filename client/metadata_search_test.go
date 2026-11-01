package client

import "testing"

func TestMetadataIDQuery(t *testing.T) {
	for q, want := range map[string]string{"38": "38", " id: 30013 ": "30013", "One Piece": "", "id:x": "", "20th Century Boys": ""} {
		got := ""
		if m := metadataIDQuery.FindStringSubmatch(q); m != nil {
			got = m[1]
		}
		if got != want {
			t.Errorf("%q: %q, want %q", q, got, want)
		}
	}
}
