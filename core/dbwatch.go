package core

import (
	"context"
	"time"

	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/storage"
)

// DBWatchEvery is how often DBWatcher looks for other processes' changes.
const DBWatchEvery = time.Second

// DBWatcher tells what changed in the data database, whichever process changed it: the
// topics (storage.Rev*) whose revision moved. Other processes' commits move SQLite's
// data_version, read every tick (nothing more when it didn't); this process's own
// writers call Poke. One per process; the TUI and serve run theirs.
type DBWatcher struct {
	db   *storage.DB
	poke chan struct{}
}

// NewDBWatcher watches db (the data database).
func NewDBWatcher(db *storage.DB) *DBWatcher {
	return &DBWatcher{db: db, poke: make(chan struct{}, 1)}
}

// Poke makes the watcher look now: this process changed something.
func (w *DBWatcher) Poke() {
	if w == nil {
		return
	}
	select {
	case w.poke <- struct{}{}:
	default:
	}
}

// Run calls changed with the topics that moved, until ctx ends.
func (w *DBWatcher) Run(ctx context.Context, changed func(topics []string)) {
	last, err := w.db.Revisions()
	if err != nil {
		log.Warn("watch the database: %s", err)
		return
	}
	version, _ := w.db.DataVersion()
	tick := time.NewTicker(DBWatchEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.poke:
		case <-tick.C:
			v, err := w.db.DataVersion()
			if err != nil || v == version {
				continue
			}
			version = v
		}
		revs, err := w.db.Revisions()
		if err != nil {
			log.Warn("watch the database: %s", err)
			continue
		}
		var topics []string
		for topic, rev := range revs {
			if rev != last[topic] {
				topics = append(topics, topic)
			}
		}
		last = revs
		if len(topics) > 0 {
			changed(topics)
		}
	}
}
