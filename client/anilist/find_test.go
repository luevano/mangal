package anilist

import (
	"context"
	"strconv"
	"testing"

	"github.com/luevano/libmangal/metadata"
)

func TestMetadataByIDsBatch(t *testing.T) {
	al := func(n int) metadata.ID {
		return metadata.ID{Raw: strconv.Itoa(n), Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}
	}
	var ids [][]metadata.ID
	for n := 1; n <= 30; n++ {
		ids = append(ids, []metadata.ID{al(n)})
	}
	// the first id unknown, the second found; a Kitsu id isn't looked up on AniList
	ids = append(ids, []metadata.ID{al(100001), al(31)}, []metadata.ID{{Raw: "7", Source: metadata.IDSourceKitsu, Code: metadata.IDCodeKitsu}}, nil)

	idRequests.Store(0)
	metas, err := MetadataByIDsBatch(context.Background(), ids)
	if err != nil {
		t.Fatal(err)
	}
	if got := idRequests.Load(); got != 2 {
		t.Errorf("%d requests for 31 ids, want 2 (25 a request)", got)
	}
	if len(metas) != len(ids) {
		t.Fatalf("%d results for %d mangas", len(metas), len(ids))
	}
	for i := range 30 {
		if metas[i] == nil || metas[i].ID().Raw != strconv.Itoa(i+1) {
			t.Errorf("manga %d: %v", i, metas[i])
		}
	}
	if metas[30] == nil || metas[30].ID().Raw != "31" || metas[31] != nil || metas[32] != nil {
		t.Errorf("got %v, %v, %v", metas[30], metas[31], metas[32])
	}

	// from the cache now
	if _, err := MetadataByIDsBatch(context.Background(), ids[:30]); err != nil {
		t.Fatal(err)
	}
	if got := idRequests.Load(); got != 2 {
		t.Errorf("%d requests, the cached ids were looked up again", got)
	}
}
