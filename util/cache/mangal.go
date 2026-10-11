package cache

const CacheDBNameMangal = "mangal"

const (
	BucketNameSearchHistory = "search-history"
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
