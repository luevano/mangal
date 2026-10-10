package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/philippgille/gokv"
	"github.com/philippgille/gokv/encoding"
	"github.com/philippgille/gokv/util"
)

var _ gokv.Store = KV{}

// KV is a gokv.Store over a kv shaped table (see kvTable), for one owner.
// Values are gob encoded, so types stored as interfaces must be registered with gob.
type KV struct {
	DB    *DB
	Table string
	Owner string
	// TTL of the values set, 0 for no expiry.
	TTL time.Duration
}

var codec = encoding.Gob

func (s KV) Set(k string, v any) error {
	if err := util.CheckKeyAndValue(k, v); err != nil {
		return err
	}
	data, err := codec.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode %s %q: %w", s.Table, k, err)
	}
	var expires int64
	if s.TTL > 0 {
		expires = time.Now().Add(s.TTL).Unix()
	}
	_, err = s.DB.Exec(`INSERT INTO `+s.Table+` (owner, key, value, expires) VALUES (?, ?, ?, ?)
ON CONFLICT (owner, key) DO UPDATE SET value = excluded.value, expires = excluded.expires`,
		s.Owner, k, data, expires)
	if err != nil {
		return fmt.Errorf("set %s %q: %w", s.Table, k, err)
	}
	return nil
}

func (s KV) Get(k string, v any) (bool, error) {
	if err := util.CheckKeyAndValue(k, v); err != nil {
		return false, err
	}
	var data []byte
	var expires int64
	err := s.DB.QueryRow(`SELECT value, expires FROM `+s.Table+` WHERE owner = ? AND key = ?`, s.Owner, k).Scan(&data, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get %s %q: %w", s.Table, k, err)
	}
	if expires != 0 && expires < time.Now().Unix() {
		return false, s.Delete(k)
	}
	if err := codec.Unmarshal(data, v); err != nil {
		return false, fmt.Errorf("decode %s %q: %w", s.Table, k, err)
	}
	return true, nil
}

func (s KV) Delete(k string) error {
	if err := util.CheckKey(k); err != nil {
		return err
	}
	if _, err := s.DB.Exec(`DELETE FROM `+s.Table+` WHERE owner = ? AND key = ?`, s.Owner, k); err != nil {
		return fmt.Errorf("delete %s %q: %w", s.Table, k, err)
	}
	return nil
}

// Close does nothing, the database stays open for the whole process.
func (s KV) Close() error {
	return nil
}
