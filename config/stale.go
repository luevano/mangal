package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/spf13/viper"
)

// Suggest is the registered key closest to key (a typo, or a key moved to another table),
// "" when none is close enough.
func Suggest(key string) string {
	key = strings.ToLower(key)
	best, distance := "", -1
	for _, k := range Keys() {
		d := fuzzy.LevenshteinDistance(key, k)
		if distance < 0 || d < distance {
			best, distance = k, d
		}
	}
	if distance >= 0 && distance <= max(2, len(key)/5) {
		return best
	}
	// the same last part, when only one key has it
	leaf := key[strings.LastIndexByte(key, '.')+1:]
	var same []string
	for _, k := range Keys() {
		if k[strings.LastIndexByte(k, '.')+1:] == leaf {
			same = append(same, k)
		}
	}
	if len(same) == 1 {
		return same[0]
	}
	return ""
}

// Stale is what config clean does to the config file.
type Stale struct {
	// Unknown are keys mangal doesn't know (removed ones, typos), dropped.
	Unknown []UnknownKey
	// Retired are keys mangal no longer uses, dropped.
	Retired []RetiredKey
	// Defaults are keys set to their default, they become commented defaults (they follow
	// mangal's default from then on).
	Defaults []string
	// Moved are keys stored in the database now, their entries are ignored: dropped once
	// the database has them (MovedKept until then).
	Moved     []MovedKey
	MovedKept bool
	// movedLeaves are the file's entries of the moved tables, by table.
	movedLeaves map[string][]string
}

// UnknownKey is a key of the file that isn't registered, Suggestion the closest one.
type UnknownKey struct {
	Key        string
	Suggestion string
}

// RetiredKey is a retired key of the file, Reason why it was removed.
type RetiredKey struct {
	Key    string
	Reason string
}

// Empty is true when there's nothing to clean.
func (s Stale) Empty() bool {
	return len(s.Unknown) == 0 && len(s.Retired) == 0 && len(s.Defaults) == 0 && (len(s.Moved) == 0 || s.MovedKept)
}

// StaleKeys reads the config file (not the environment or flags) for what config clean
// would change. No file is nothing to clean.
func StaleKeys() (Stale, error) {
	var s Stale
	file := viper.New()
	file.SetFs(afs.Afero.Fs)
	file.SetConfigFile(Path)
	if err := file.ReadInConfig(); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return s, nil
		}
		return s, fmt.Errorf("read config %q: %w", Path, err)
	}
	moved := map[string]any{}
	for _, key := range movedKeys() {
		if file.IsSet(key) {
			moved[key] = nil
			s.Moved = append(s.Moved, MovedKey{Key: key, NewKey: newName(key)})
		}
	}
	s.MovedKept = len(s.Moved) != 0 && !Imported()
	s.movedLeaves = map[string][]string{}
	for _, key := range file.AllKeys() {
		if underMoved(key, moved) {
			for m := range moved {
				if strings.HasPrefix(key, m+".") {
					s.movedLeaves[m] = append(s.movedLeaves[m], key)
				}
			}
			continue
		}
		if reason, ok := retired[key]; ok {
			s.Retired = append(s.Retired, RetiredKey{Key: key, Reason: reason})
			continue
		}
		if !Exists(key) {
			if !underMap(key) {
				s.Unknown = append(s.Unknown, UnknownKey{Key: key, Suggestion: Suggest(key)})
			}
			continue
		}
		// an invalid value isn't the default, Validate reports it
		if raw, err := fields[key].toRaw(file.Get(key)); err == nil && isDefault(key, raw) {
			s.Defaults = append(s.Defaults, key)
		}
	}
	slices.SortFunc(s.Unknown, func(a, b UnknownKey) int { return strings.Compare(a.Key, b.Key) })
	slices.SortFunc(s.Retired, func(a, b RetiredKey) int { return strings.Compare(a.Key, b.Key) })
	slices.Sort(s.Defaults)
	slices.SortFunc(s.Moved, func(a, b MovedKey) int { return strings.Compare(a.Key, b.Key) })
	return s, nil
}

// Clean drops the unknown, retired and moved (once imported) keys of s from the config
// file and comments its
// defaults out, keeping the rest of the file (comments included). The file is copied to
// backup first (mangal.toml.<time>.bak next to it, never overwritten).
func Clean(s Stale) (backup string, err error) {
	if s.Empty() {
		return "", nil
	}
	if !isTOML() {
		return "", fmt.Errorf("clean config: only a TOML file can be edited in place (%s)", Path)
	}
	data, err := afs.Afero.ReadFile(Path)
	if err != nil {
		return "", fmt.Errorf("clean config: %w", err)
	}
	edited := data
	var drop []string
	for _, k := range s.Unknown {
		drop = append(drop, k.Key)
	}
	for _, k := range s.Retired {
		drop = append(drop, k.Key)
	}
	for _, key := range drop {
		next, err := removeTOMLKey(edited, key)
		if err == nil {
			err = checkTOMLRemoved(edited, next, key)
		}
		if err != nil {
			return "", fmt.Errorf("clean config: drop %s: %w (edit it by hand)", key, err)
		}
		edited = next
	}
	if !s.MovedKept {
		for _, m := range s.Moved {
			next, err := removeMoved(edited, m.Key, s.movedLeaves[m.Key])
			if err != nil {
				return "", fmt.Errorf("clean config: drop %s: %w (edit it by hand)", m.Key, err)
			}
			edited = next
		}
	}
	for _, key := range s.Defaults {
		next, err := defaultTOML(edited, key, fields[key].defaultRaw())
		if err == nil {
			err = checkTOMLRemoved(edited, next, key)
		}
		if err != nil {
			return "", fmt.Errorf("clean config: comment %s out: %w (edit it by hand)", key, err)
		}
		edited = next
	}
	if bytes.Equal(edited, data) {
		return "", nil
	}
	backup, err = backupPath(time.Now())
	if err != nil {
		return "", fmt.Errorf("clean config: %w", err)
	}
	if err := afs.WriteFileAtomic(backup, data, 0o600); err != nil {
		return "", fmt.Errorf("clean config: back up: %w", err)
	}
	if err := writeFile(edited); err != nil {
		return backup, fmt.Errorf("clean config: %w", err)
	}
	return backup, nil
}

// backupPath is a backup of the config file that doesn't exist yet.
func backupPath(now time.Time) (string, error) {
	base := Path + "." + now.Format("2006-01-02T150405")
	for i := range 100 {
		path := base + ".bak"
		if i > 0 {
			path = fmt.Sprintf("%s-%d.bak", base, i)
		}
		if _, err := afs.Afero.Stat(path); errors.Is(err, fs.ErrNotExist) {
			return path, nil
		}
	}
	return "", fmt.Errorf("no free backup name for %s", Path)
}

// removeMoved drops a moved key: its entry, or a table's entries (leaves).
func removeMoved(data []byte, key string, leaves []string) ([]byte, error) {
	next, err := removeTOMLKey(data, key)
	if err == nil && !bytes.Equal(next, data) {
		return next, checkTOMLRemoved(data, next, key)
	}
	for _, leaf := range leaves {
		next, err := removeTOMLKey(data, leaf)
		if err == nil {
			err = checkTOMLRemoved(data, next, leaf)
		}
		if err != nil {
			return nil, err
		}
		data = next
	}
	return data, nil
}
