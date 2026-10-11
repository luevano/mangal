package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/luevano/libmangal/metadata"
	mango "github.com/luevano/mangoprovider"
)

// Owners and buckets of mangal's own data, as Store gets them.
const (
	OwnerMangal         = "mangal"
	BucketSearchHistory = "search-history"

	OwnerMangalAuth   = "mangal-auth"
	BucketAuthHistory = "auth-history"

	// OwnerAccounts is the cached provider accounts in TableOther, by provider id.
	OwnerAccounts = "accounts"
)

// metadataTTL is how long metadata searches and mangas are cached.
const metadataTTL = 48 * time.Hour

// Storage is mangal's two databases.
type Storage struct {
	Cache, Data *DB
}

// StoreOptions are the user's settings Store needs.
type StoreOptions struct {
	// TTL of the providers' cache (cache.ttl).
	TTL time.Duration
	// MirrorTTL of the mirror that last worked (cache.mirror_ttl).
	MirrorTTL time.Duration
	// Keyring keeps the logins in the OS keyring (auth.keyring).
	Keyring bool
}

// Store is the store of an owner (a provider id, a metadata provider id, or mangal's own)
// and bucket (the kind of data), as libmangal and the providers ask for them.
//
// Each kind of data has its own table, in the cache database or, for what isn't cache
// (title bindings, logins, search history), in the data database. The TTL is decided by
// the kind.
func (s Storage) Store(owner, bucket string, opts StoreOptions) KV {
	switch {
	case owner == OwnerMangalAuth && bucket == BucketAuthHistory:
		return KV{DB: s.Data, Table: TableAuthHistory}
	case owner == OwnerMangalAuth:
		// no expiry, an expired token is kept (marked) to tell the user
		return s.AuthStore(bucket, opts.Keyring)
	case owner == OwnerMangal && bucket == BucketSearchHistory:
		return KV{DB: s.Data, Table: TableSearchHistory}
	case bucket == metadata.CacheBucketNameTitleToID:
		return KV{DB: s.Data, Table: TableTitleBindings, Owner: owner}
	case bucket == metadata.CacheBucketNameQueryToIDs:
		return KV{DB: s.Cache, Table: TableMetadataSearches, Owner: owner, TTL: metadataTTL}
	case bucket == metadata.CacheBucketNameIDToManga:
		return KV{DB: s.Cache, Table: TableMetadata, Owner: owner, TTL: metadataTTL}
	case bucket == mango.CacheBucketNameMangas:
		return KV{DB: s.Cache, Table: TableMangaSearches, Owner: owner, TTL: opts.TTL}
	case bucket == mango.CacheBucketNameVolumes:
		return KV{DB: s.Cache, Table: TableVolumes, Owner: owner, TTL: opts.TTL}
	case bucket == mango.CacheBucketNameChapters:
		return KV{DB: s.Cache, Table: TableChapters, Owner: owner, TTL: opts.TTL}
	case bucket == mango.CacheBucketNameMirrors:
		return KV{DB: s.Cache, Table: TableOther, Owner: owner + "/" + bucket, TTL: opts.MirrorTTL}
	case bucket == owner:
		// luaprovider caches http responses with the provider id as the bucket
		return KV{DB: s.Cache, Table: TableLuaHTTP, Owner: owner, TTL: opts.TTL}
	default:
		return KV{DB: s.Cache, Table: TableOther, Owner: owner + "/" + bucket, TTL: opts.TTL}
	}
}

// AuthStore is the logins of the owner (e.g. "anilist"), in the OS keyring when keyring.
func (s Storage) AuthStore(owner string, keyring bool) KV {
	return KV{DB: s.Data, Table: TableAuthTokens, Owner: owner, Keyring: keyring}
}

// AccountsStore is the cached provider accounts, by provider id.
func (s Storage) AccountsStore(ttl time.Duration) KV {
	return KV{DB: s.Cache, Table: TableOther, Owner: OwnerAccounts, TTL: ttl}
}

// Library is the library in the data database.
func (s Storage) Library() Library {
	return Library{DB: s.Data}
}

// History is the read history in the data database.
func (s Storage) History() History {
	return History{DB: s.Data}
}

// Watches are the watched mangas and their checks in the data database.
func (s Storage) Watches() Watches {
	return Watches{DB: s.Data}
}

// Bindings are the metadata bindings of the provider mangas.
func (s Storage) Bindings() Bindings {
	return Bindings{DB: s.Data}
}

// ClearCache deletes the cache entries of a provider (or metadata provider) id, or all of
// them if the id is empty. It returns how many were deleted.
func (s Storage) ClearCache(id string) (int64, error) {
	return ClearCache(s.Cache, id)
}

// providerRows matches the rows of a cache table that belong to the provider (or metadata
// provider) id ?1, see Store: owned by it, by "<id>/<bucket>", or its cached account.
const providerRows = `(owner = ?1 OR substr(owner, 1, length(?1) + 1) = ?1 || '/' OR (owner = '` + OwnerAccounts + `' AND key = ?1))`

// ClearCache deletes the cache entries of a provider (or metadata provider) id, or all of
// them if the id is empty. It returns how many were deleted.
func ClearCache(cache *DB, id string) (int64, error) {
	var total int64
	err := cache.InTx(func(tx *sql.Tx) error {
		total = 0
		for _, table := range CacheTables {
			query, args := `DELETE FROM `+table, []any{}
			if id != "" {
				query, args = query+` WHERE `+providerRows, []any{id}
			}
			res, err := tx.Exec(query, args...)
			if err != nil {
				return fmt.Errorf("clear %s: %w", table, err)
			}
			n, _ := res.RowsAffected()
			total += n
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}

// countProviderRows is how many cache entries belong to the provider id.
func countProviderRows(cache *DB, id string) (int, error) {
	total := 0
	for _, table := range CacheTables {
		var n int
		if err := cache.QueryRow(`SELECT count(*) FROM `+table+` WHERE `+providerRows, id).Scan(&n); err != nil {
			return 0, fmt.Errorf("count %s: %w", table, err)
		}
		total += n
	}
	return total, nil
}

// ClearChapterCache deletes the cached volumes, chapters and Lua http responses of a provider,
// so its next chapter lists are fetched (e.g. to check for new chapters). It returns how
// many were deleted.
func (s Storage) ClearChapterCache(id string) (int64, error) {
	var total int64
	err := s.Cache.InTx(func(tx *sql.Tx) error {
		total = 0
		for _, table := range []string{TableVolumes, TableChapters, TableLuaHTTP} {
			res, err := tx.Exec(`DELETE FROM `+table+` WHERE owner = ?`, id)
			if err != nil {
				return fmt.Errorf("clear %s of %s: %w", table, id, err)
			}
			n, _ := res.RowsAffected()
			total += n
		}
		return nil
	})
	return total, err
}

// AuthFingerprint changes whenever a login or secret is saved or removed (the auth_tokens
// table), so a long-running process can tell its providers were made with older ones. With
// auth.keyring the values are in the keyring and only their rows are seen.
func (s Storage) AuthFingerprint() (string, error) {
	rows, err := s.Data.Query(`SELECT owner, key, value FROM ` + TableAuthTokens + ` ORDER BY owner, key`)
	if err != nil {
		return "", fmt.Errorf("auth fingerprint: %w", err)
	}
	defer rows.Close()
	h := sha256.New()
	for rows.Next() {
		var owner, key string
		var value []byte
		if err := rows.Scan(&owner, &key, &value); err != nil {
			return "", fmt.Errorf("auth fingerprint: %w", err)
		}
		fmt.Fprintf(h, "%s\x00%s\x00%d\x00", owner, key, len(value))
		h.Write(value)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("auth fingerprint: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
