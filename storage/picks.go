package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/luevano/libmangal/metadata"
)

// linkPicksTable: the links of a library manga its sources disagree on (a MangaDex Kitsu
// link against Kitsu's own AniList mapping), kept until one is linked or it's dismissed.
const linkPicksTable = `CREATE TABLE link_picks (
	manga      INTEGER NOT NULL REFERENCES library_mangas (id) ON DELETE CASCADE,
	source     TEXT NOT NULL,
	anilist    TEXT NOT NULL DEFAULT '',
	candidates TEXT NOT NULL,
	found      INTEGER NOT NULL,
	dismissed  INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (manga, source)
)`

// LinkPick is a link of a library manga to pick: its sources give different ids, nothing
// is linked until one is picked.
type LinkPick struct {
	Manga int64 `json:"manga"`
	// Source is the metadata source to link (kt).
	Source metadata.IDCode `json:"source"`
	// AniList is the AniList id the candidates were found for.
	AniList    string          `json:"anilist,omitempty"`
	Candidates []LinkCandidate `json:"candidates"`
	Found      time.Time       `json:"found"`
	// Dismissed is true when the user chose neither: the same candidates aren't asked again.
	Dismissed bool `json:"dismissed,omitempty"`
}

// LinkCandidate is an id one of the sources gives.
type LinkCandidate struct {
	ID string `json:"id"`
	// From is where it comes from: a provider id (its links), or kitsu (Kitsu's mapping).
	From string `json:"from"`
}

// FromKitsu is LinkCandidate.From of an id Kitsu maps the AniList id to.
const FromKitsu = "kitsu"

// FromName is From for people: "Kitsu mapping" or the provider.
func (c LinkCandidate) FromName() string {
	if c.From == FromKitsu {
		return "Kitsu mapping"
	}
	return c.From
}

// IDs are the candidates' ids.
func (p LinkPick) IDs() []string {
	ids := make([]string, len(p.Candidates))
	for i, c := range p.Candidates {
		ids[i] = c.ID
	}
	return ids
}

// SetLinkPick saves the pick, replacing the source's one, and returns it as saved: a
// dismissed pick with the same candidates stays dismissed.
func (l Library) SetLinkPick(p LinkPick) (LinkPick, error) {
	if p.Found.IsZero() {
		p.Found = time.Now()
	}
	data, err := json.Marshal(p.Candidates)
	if err != nil {
		return p, err
	}
	err = l.DB.InTx(func(tx *sql.Tx) error {
		old, found, err := linkPick(tx, p.Manga, p.Source)
		if err != nil {
			return err
		}
		p.Dismissed = p.Dismissed || found && old.Dismissed && slices.Equal(old.Candidates, p.Candidates)
		_, err = tx.Exec(`INSERT INTO link_picks (manga, source, anilist, candidates, found, dismissed) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (manga, source) DO UPDATE SET anilist = excluded.anilist, candidates = excluded.candidates, found = excluded.found, dismissed = excluded.dismissed`,
			p.Manga, p.Source, p.AniList, string(data), unix(p.Found), p.Dismissed)
		return err
	})
	if err != nil {
		return p, fmt.Errorf("save the %s pick of library manga %d: %w", p.Source, p.Manga, err)
	}
	return p, nil
}

// LinkPick is the library manga's pick of the source, false if it has none (a dismissed
// one is returned too).
func (l Library) LinkPick(manga int64, source metadata.IDCode) (LinkPick, bool, error) {
	p, found, err := linkPick(l.DB, manga, source)
	if err != nil {
		return LinkPick{}, false, fmt.Errorf("the %s pick of library manga %d: %w", source, manga, err)
	}
	return p, found, nil
}

func linkPick(q querier, manga int64, source metadata.IDCode) (LinkPick, bool, error) {
	picks, err := queryAll(q, scanLinkPick, `SELECT `+linkPickColumns+` FROM link_picks WHERE manga = ? AND source = ?`, manga, source)
	if err != nil || len(picks) == 0 {
		return LinkPick{}, false, err
	}
	return picks[0], true, nil
}

const linkPickColumns = `manga, source, anilist, candidates, found, dismissed`

func scanLinkPick(rows *sql.Rows) (LinkPick, error) {
	var p LinkPick
	var candidates string
	var found int64
	if err := rows.Scan(&p.Manga, &p.Source, &p.AniList, &candidates, &found, &p.Dismissed); err != nil {
		return p, err
	}
	p.Found = fromUnix(found)
	return p, json.Unmarshal([]byte(candidates), &p.Candidates)
}

// LinkPicks are the picks not dismissed, by library manga and source.
func (l Library) LinkPicks() ([]LinkPick, error) {
	picks, err := queryAll(l.DB, scanLinkPick, `SELECT `+linkPickColumns+` FROM link_picks WHERE NOT dismissed ORDER BY manga, source`)
	if err != nil {
		return nil, fmt.Errorf("list the link picks: %w", err)
	}
	return picks, nil
}

// DismissLinkPick keeps the pick without asking it again (until other candidates are
// found), false if there's none.
func (l Library) DismissLinkPick(manga int64, source metadata.IDCode) (bool, error) {
	res, err := l.DB.Exec(`UPDATE link_picks SET dismissed = 1 WHERE manga = ? AND source = ?`, manga, source)
	if err != nil {
		return false, fmt.Errorf("dismiss the %s pick of library manga %d: %w", source, manga, err)
	}
	n, _ := res.RowsAffected()
	return n != 0, nil
}

// ClearLinkPick forgets the pick (dismissed or not).
func (l Library) ClearLinkPick(manga int64, source metadata.IDCode) error {
	if _, err := l.DB.Exec(`DELETE FROM link_picks WHERE manga = ? AND source = ?`, manga, source); err != nil {
		return fmt.Errorf("clear the %s pick of library manga %d: %w", source, manga, err)
	}
	return nil
}

// clearPicks forgets the picks a link change settles: a source linked, any pick when the
// AniList link changes (they were found for it).
func clearPicks(tx *sql.Tx, manga int64, links map[metadata.IDCode]string) error {
	for code, id := range links {
		var err error
		switch {
		case code == metadata.IDCodeAnilist:
			_, err = tx.Exec(`DELETE FROM link_picks WHERE manga = ? AND anilist != ?`, manga, id)
		case id != "":
			_, err = tx.Exec(`DELETE FROM link_picks WHERE manga = ? AND source = ?`, manga, code)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// clearSourcePicks forgets the picks of the source of the library mangas the provider's
// manga is a source of: bound on its own (mangal bind), it's linked.
func clearSourcePicks(tx *sql.Tx, provider, mangaID string, source metadata.IDCode) error {
	_, err := tx.Exec(`DELETE FROM link_picks WHERE source = ? AND manga IN (SELECT manga FROM library_sources WHERE provider = ? AND manga_id = ?)`, source, provider, mangaID)
	return err
}
