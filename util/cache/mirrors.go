package cache

import mango "github.com/luevano/mangoprovider"

// DeleteMirror forgets the mirror that last worked for the provider, so its preferred one is
// tried first again.
func DeleteMirror(providerID string) error {
	store, err := CacheStore(providerID, mango.CacheBucketNameMirrors)
	if err != nil {
		return err
	}
	return store.Delete(providerID)
}
