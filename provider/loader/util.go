package loader

import (
	"log"
	"path/filepath"
	"time"

	"github.com/luevano/mangal/config"
	"github.com/luevano/mangal/path"
	"github.com/luevano/mangal/util/cache/bbolt"
	"github.com/philippgille/gokv"
	"github.com/philippgille/gokv/encoding"
)

func httpStoreProvider(providerID string) (gokv.Store, error) {
	ttl, err := time.ParseDuration(config.Config.Providers.Cache.TTL.Get())
	if err != nil {
		log.Fatal(err)
	}

	return bbolt.NewStore(bbolt.Options{
		TTL:        ttl,
		BucketName: providerID,
		Path:       filepath.Join(path.CacheDir(), providerID+".db"),
		Codec:      encoding.Gob,
	})
}