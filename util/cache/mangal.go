package cache

import "github.com/luevano/mangal/v5/storage"

const CacheDBNameMangal = storage.OwnerMangal

const (
	BucketNameSearchHistory = storage.BucketSearchHistory
)

const (
	MangasSearchHistory  = "mangas"
	AnilistSearchHistory = "anilist"
)

// get and set go through the store each time: mangal's stores are views over the
// databases, opened once per process, so there's nothing to keep open.
func get(dbName, bucketName, key string, v any) (bool, error) {
	store, err := CacheStore(dbName, bucketName)
	if err != nil {
		return false, err
	}
	return store.Get(key, v)
}

func set(dbName, bucketName, key string, v any) error {
	store, err := CacheStore(dbName, bucketName)
	if err != nil {
		return err
	}
	return store.Set(key, v)
}

// SetMangasSearchHistory will store the manga search history records to the cache.
func SetMangasSearchHistory(records Records) error {
	return set(CacheDBNameMangal, BucketNameSearchHistory, MangasSearchHistory, records)
}

// GetMangasSearchHistory will populate the given records from the manga search history cache.
func GetMangasSearchHistory(records *Records) (bool, error) {
	return get(CacheDBNameMangal, BucketNameSearchHistory, MangasSearchHistory, records)
}

// SetAnilistSearchHistory will store the manga search history records to the cache.
func SetAnilistSearchHistory(records Records) error {
	return set(CacheDBNameMangal, BucketNameSearchHistory, AnilistSearchHistory, records)
}

// GetAnilistSearchHistory will populate the given records from the manga search history cache.
func GetAnilistSearchHistory(records *Records) (bool, error) {
	return get(CacheDBNameMangal, BucketNameSearchHistory, AnilistSearchHistory, records)
}

// FillProviders is the key of the providers a fill searched last (the TUI's choice).
const FillProviders = "fill-providers"

// SetFillProviders stores the provider ids a fill searched.
func SetFillProviders(ids []string) error {
	return set(CacheDBNameMangal, BucketNameSearchHistory, FillProviders, ids)
}

// GetFillProviders reads the provider ids a fill searched last, false when there are none.
func GetFillProviders(ids *[]string) (bool, error) {
	return get(CacheDBNameMangal, BucketNameSearchHistory, FillProviders, ids)
}
