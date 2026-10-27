package cache

import (
	"testing"

	"github.com/luevano/mangal/v5/storage"
)

// TestClaimUserData: the logins and search history of before 5.28 become the local user's,
// a rename takes them along, and AuthAccount is the account a user's reads sync to.
func TestClaimUserData(t *testing.T) {
	store, err := CacheStore(CacheDBNameMangalAuth, BucketNameAuthHistory)
	if err != nil {
		t.Fatal(err)
	}
	// nobody's, as before 5.28
	if err := store.Set(AnilistAuthHistory, UserHistory{"old", "me"}); err != nil {
		t.Fatal(err)
	}
	search, err := CacheStore(CacheDBNameMangal, BucketNameSearchHistory)
	if err != nil {
		t.Fatal(err)
	}
	if err := search.Set(MangasSearchHistory, Records{{Query: "berserk"}}); err != nil {
		t.Fatal(err)
	}
	local, err := LocalUser()
	if err != nil || local != storage.DefaultUser {
		t.Fatalf("local user %q %v", local, err)
	}
	if err := ClaimUserData("", local); err != nil {
		t.Fatal(err)
	}
	var history UserHistory
	if found, err := GetAuthHistory(AnilistAuthHistory, &history); !found || err != nil || history.Last() != "me" {
		t.Errorf("local's accounts %v %v %v", history, found, err)
	}
	var records Records
	if found, _ := GetMangasSearchHistory(&records); !found || len(records) != 1 {
		t.Errorf("local's searches %v", records)
	}
	if account, err := AuthAccount(local, AnilistAuthHistory); err != nil || account != "me" {
		t.Errorf("account %q %v", account, err)
	}
	if err := SetAnilistLoggedOut(true); err != nil {
		t.Fatal(err)
	}
	if account, _ := AuthAccount(local, AnilistAuthHistory); account != "" {
		t.Errorf("logged out but %q", account)
	}

	if err := ClaimUserData(local, "kid"); err != nil {
		t.Fatal(err)
	}
	if found, _ := GetAuthHistory(AnilistAuthHistory, &history); found {
		t.Error("the local user still has the renamed user's accounts")
	}
	if out, _ := loggedOutOf("kid", anilistLoggedOut); !out {
		t.Error("the logout didn't follow the rename")
	}
}

func loggedOutOf(user, key string) (bool, error) {
	var out bool
	_, err := get(CacheDBNameMangalAuth, BucketNameAuthHistory, user+"/"+key, &out)
	return out, err
}
