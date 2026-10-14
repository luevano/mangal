package log

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

// timeLayout is the time of a line in the files.
const timeLayout = "2006-01-02T15:04:05.000"

// Entry is a logged message of a file.
type Entry struct {
	Time  time.Time
	Level Level
	// Message may have more lines.
	Message string
	// File is the name of its file.
	File string
}

// String is the entry as in the file.
func (e Entry) String() string {
	return e.Time.Format(timeLayout) + " " + padLevel(e.Level) + " " + e.Message
}

func padLevel(l Level) string {
	return (l.String() + "     ")[:5]
}

// parseLine is the entry a line starts, ok is false for a line that continues the last.
func parseLine(line string) (Entry, bool) {
	// 2026-10-13T15:04:05.000 LEVEL message
	if len(line) < len(timeLayout)+7 || line[len(timeLayout)] != ' ' {
		return Entry{}, false
	}
	t, err := time.ParseInLocation(timeLayout, line[:len(timeLayout)], time.Local)
	if err != nil {
		return Entry{}, false
	}
	rest := line[len(timeLayout)+1:]
	level, err := ParseLevel(strings.TrimRight(rest[:5], " "))
	if err != nil || rest[5] != ' ' {
		return Entry{}, false
	}
	return Entry{Time: t, Level: level, Message: rest[6:]}, true
}

// Filter picks entries, the zero Filter picks all.
type Filter struct {
	// Since and Until bound the entries' time, zero for no bound.
	Since, Until time.Time
	// Level is the lowest level.
	Level Level
	// Pattern matches the message (or the level), nil for any.
	Pattern *regexp.Regexp
}

// Match is whether the filter picks the entry.
func (f Filter) Match(e Entry) bool {
	if e.Level < f.Level {
		return false
	}
	if !f.Since.IsZero() && e.Time.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && e.Time.After(f.Until) {
		return false
	}
	return f.Pattern == nil || f.Pattern.MatchString(e.Message)
}

// overlaps is whether the file may have entries the filter picks (by its period and last
// write).
func (f Filter) overlaps(file File) bool {
	if !f.Since.IsZero() && (file.ModTime.Before(f.Since) && file.End.Before(f.Since)) {
		return false
	}
	return f.Until.IsZero() || !file.Start.After(f.Until)
}

// Pattern compiles a search pattern: a regexp, case-insensitive unless it has a capital
// letter or caseSensitive. An invalid regexp is searched as text.
func Pattern(expr string, caseSensitive bool) *regexp.Regexp {
	if expr == "" {
		return nil
	}
	prefix := ""
	if !caseSensitive && strings.ToLower(expr) == expr {
		prefix = "(?i)"
	}
	re, err := regexp.Compile(prefix + expr)
	if err != nil {
		re = regexp.MustCompile(prefix + regexp.QuoteMeta(expr))
	}
	return re
}

// Scan calls fn with the entries of the files the filter picks, in order. A file deleted
// meanwhile (compressed, expired) is skipped; a stop error of fn stops it, returned.
func Scan(ctx context.Context, files []File, filter Filter, fn func(Entry) error) error {
	for _, f := range files {
		if !filter.overlaps(f) {
			continue
		}
		if err := scanFile(ctx, f, filter, fn); err != nil {
			return err
		}
	}
	return nil
}

func scanFile(ctx context.Context, f File, filter Filter, fn func(Entry) error) error {
	r, err := openRead(f)
	if errors.Is(err, os.ErrNotExist) && !f.Compressed {
		// compressed meanwhile
		f.Compressed, f.Path = true, f.Path+".gz"
		r, err = openRead(f)
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer r.Close()
	p := parser{file: f.Name, filter: filter, fn: fn}
	br := bufio.NewReaderSize(r, 64*1024)
	for n := 0; ; n++ {
		if n%1024 == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		line, err := br.ReadString('\n')
		if line != "" {
			if err := p.line(strings.TrimSuffix(line, "\n")); err != nil {
				return err
			}
		}
		if err == io.EOF {
			return p.flush()
		}
		if err != nil {
			return err
		}
	}
}

// openRead opens the file for reading, gunzipped.
func openRead(f File) (io.ReadCloser, error) {
	file, err := os.Open(f.Path)
	if err != nil || !f.Compressed {
		return file, err
	}
	zr, err := gzip.NewReader(file)
	if err != nil {
		file.Close()
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{zr, file}, nil
}

// parser makes entries of lines, a line that isn't an entry's goes to the last one.
type parser struct {
	file    string
	filter  Filter
	fn      func(Entry) error
	pending *Entry
}

func (p *parser) line(line string) error {
	e, ok := parseLine(line)
	if !ok {
		if p.pending != nil {
			p.pending.Message += "\n" + line
			return nil
		}
		// no entry to continue (the start of the file was cut): one of its own
		e = Entry{Level: LevelInfo, Message: line}
	}
	if err := p.flush(); err != nil {
		return err
	}
	e.File = p.file
	p.pending = &e
	return nil
}

func (p *parser) flush() error {
	if p.pending == nil {
		return nil
	}
	e := *p.pending
	p.pending = nil
	if !p.filter.Match(e) {
		return nil
	}
	return p.fn(e)
}

// Tail reads what's appended to the newest log file of a dir, moving on to newer files
// as they come (another period, segment).
type Tail struct {
	dir    string
	file   File
	offset int64
	// rest is an incomplete last line
	rest string
}

// NewTail tails dir from the end of its newest file (or the first file that comes).
func NewTail(dir string) (*Tail, error) {
	files, err := Files(dir)
	if err != nil {
		return nil, err
	}
	t := &Tail{dir: dir}
	if len(files) > 0 {
		t.file = files[len(files)-1]
		t.offset = t.file.Size
	}
	return t, nil
}

// NewTailAt tails dir from the start of f (one of its Files), then the newer files.
func NewTailAt(dir string, f File) *Tail {
	return &Tail{dir: dir, file: f}
}

// Next calls fn with the entries the filter picks appended since the last call.
func (t *Tail) Next(filter Filter, fn func(Entry) error) error {
	for {
		if t.file.Name != "" && !t.file.Compressed {
			if err := t.read(filter, fn); err != nil {
				return err
			}
		}
		files, err := Files(t.dir)
		if err != nil {
			return err
		}
		i := 0
		if t.file.Name != "" {
			i = len(files)
			for j, f := range files {
				if compareFiles(f, t.file) > 0 && f.Name != t.file.Name+".gz" {
					i = j
					break
				}
			}
		}
		if i >= len(files) {
			return nil
		}
		// a newer file: the rest of this one was read, from its start
		t.file, t.offset, t.rest = files[i], 0, ""
	}
}

// Following is the name of the file the tail reads.
func (t *Tail) Following() string { return t.file.Name }

func (t *Tail) read(filter Filter, fn func(Entry) error) error {
	f, err := os.Open(t.file.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	t.offset += int64(len(data))
	text := t.rest + string(data)
	end := strings.LastIndexByte(text, '\n')
	if end < 0 {
		t.rest = text
		return nil
	}
	t.rest = text[end+1:]
	// a write is one entry (with its lines): the chunk ends at an entry's end
	p := parser{file: t.file.Name, filter: filter, fn: fn}
	for line := range strings.SplitSeq(text[:end], "\n") {
		if err := p.line(line); err != nil {
			return err
		}
	}
	return p.flush()
}

// Follow calls fn with the entries the filter picks as they're logged to dir, polling every
// interval, until ctx is done.
func Follow(ctx context.Context, dir string, interval time.Duration, filter Filter, fn func(Entry) error) error {
	t, err := NewTail(dir)
	if err != nil {
		return err
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if err := t.Next(filter, fn); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}
