package core

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/luevano/mangal/v5/log"
)

// Logs reads the log files of every mangal process.
type Logs interface {
	// Files are the log files, oldest first.
	Files(ctx context.Context) ([]log.File, error)
	// Entries tells fn the entries of the query, oldest first.
	Entries(ctx context.Context, q LogQuery, fn func(log.Entry) error) error
	// Follow tells fn the entries of the query written from now on, until ctx is done.
	Follow(ctx context.Context, q LogQuery, fn func(log.Entry) error) error
}

// LogQuery are the log entries asked for.
type LogQuery struct {
	// Files are the names of the files read, none for every file (Follow: ignored).
	Files []string `json:"files,omitempty"`
	// Since and Until bound the entries' time, zero for no bound.
	Since time.Time `json:"since,omitzero"`
	Until time.Time `json:"until,omitzero"`
	// Level is the lowest level (debug, info, warn, error), "" for every one.
	Level string `json:"level,omitempty"`
	// Grep is a regexp the entries match, case-insensitive unless it has a capital
	// letter or CaseSensitive.
	Grep          string `json:"grep,omitempty"`
	CaseSensitive bool   `json:"case_sensitive,omitempty"`
	// Limit is the last entries kept, 0 for all, MaxLogLimit at most.
	Limit int `json:"limit,omitempty"`
}

// MaxLogLimit is the most entries a LogQuery's Limit keeps (they are all held in memory).
const MaxLogLimit = 10000

func (q LogQuery) filter() (log.Filter, error) {
	f := log.Filter{Since: q.Since, Until: q.Until, Pattern: log.Pattern(q.Grep, q.CaseSensitive)}
	if q.Level != "" {
		level, err := log.ParseLevel(q.Level)
		if err != nil {
			return f, invalid("level: %s", err)
		}
		f.Level = level
	}
	return f, nil
}

// followInterval is how often the files are looked at for new entries.
const followInterval = 500 * time.Millisecond

// NewLogs are the logs of the directory, without the rest of the services (no storage).
func NewLogs(dir func() string) Logs { return &localLogs{dir: dir} }

type localLogs struct {
	dir func() string
}

func (l *localLogs) Files(context.Context) ([]log.File, error) {
	files, err := log.Files(l.dir())
	if err != nil {
		return nil, fmt.Errorf("list log files: %w", err)
	}
	return files, nil
}

func (l *localLogs) Entries(ctx context.Context, q LogQuery, fn func(log.Entry) error) error {
	filter, err := q.filter()
	if err != nil {
		return err
	}
	files, err := l.Files(ctx)
	if err != nil {
		return err
	}
	if len(q.Files) != 0 {
		var picked []log.File
		for _, name := range q.Files {
			i := slices.IndexFunc(files, func(f log.File) bool { return f.Name == name || f.Name == name+".gz" })
			if i < 0 {
				return &NotFoundError{What: fmt.Sprintf("no log file %q", name)}
			}
			picked = append(picked, files[i])
		}
		files = picked
	}
	if q.Limit > MaxLogLimit {
		return invalid("limit: %d is over %d", q.Limit, MaxLogLimit)
	}
	if q.Limit <= 0 {
		return log.Scan(ctx, files, filter, fn)
	}
	// the last ones: a ring of them
	last := make([]log.Entry, q.Limit)
	n := 0
	err = log.Scan(ctx, files, filter, func(e log.Entry) error {
		last[n%len(last)] = e
		n++
		return nil
	})
	for i := max(0, n-len(last)); err == nil && i < n; i++ {
		err = fn(last[i%len(last)])
	}
	return err
}

func (l *localLogs) Follow(ctx context.Context, q LogQuery, fn func(log.Entry) error) error {
	filter, err := q.filter()
	if err != nil {
		return err
	}
	return log.Follow(ctx, l.dir(), followInterval, filter, fn)
}
