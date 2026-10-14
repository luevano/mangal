package log

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Period is how often a new log file starts.
type Period int

const (
	Daily Period = iota
	Hourly
	Weekly
	Monthly
)

// Periods are the names of the periods, for the config.
var Periods = []string{"hourly", "daily", "weekly", "monthly"}

func (p Period) String() string {
	switch p {
	case Hourly:
		return "hourly"
	case Weekly:
		return "weekly"
	case Monthly:
		return "monthly"
	default:
		return "daily"
	}
}

// ParsePeriod is the Period of a name of Periods.
func ParsePeriod(s string) (Period, error) {
	for _, p := range []Period{Hourly, Daily, Weekly, Monthly} {
		if p.String() == s {
			return p, nil
		}
	}
	return Daily, fmt.Errorf("%q isn't one of %s", s, strings.Join(Periods, ", "))
}

// stamp is the name of t's period: 2026-10-13T15, 2026-10-13, 2026-W42, 2026-10.
func (p Period) stamp(t time.Time) string {
	switch p {
	case Hourly:
		return t.Format("2006-01-02T15")
	case Weekly:
		y, w := t.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", y, w)
	case Monthly:
		return t.Format("2006-01")
	default:
		return t.Format(time.DateOnly)
	}
}

var weekStamp = regexp.MustCompile(`^(\d{4})-W(\d{2})$`)

// parseStamp is the start and end of a period's stamp (any Period's), local time.
func parseStamp(s string) (start, end time.Time, ok bool) {
	if t, err := time.ParseInLocation("2006-01-02T15", s, time.Local); err == nil {
		return t, t.Add(time.Hour), true
	}
	if t, err := time.ParseInLocation(time.DateOnly, s, time.Local); err == nil {
		return t, t.AddDate(0, 0, 1), true
	}
	if t, err := time.ParseInLocation("2006-01", s, time.Local); err == nil {
		return t, t.AddDate(0, 1, 0), true
	}
	if m := weekStamp.FindStringSubmatch(s); m != nil {
		y, _ := strconv.Atoi(m[1])
		w, _ := strconv.Atoi(m[2])
		if w < 1 || w > 53 {
			return start, end, false
		}
		// the monday of ISO week 1 is the one of the week with january 4th
		jan4 := time.Date(y, 1, 4, 0, 0, 0, 0, time.Local)
		monday := jan4.AddDate(0, 0, -((int(jan4.Weekday())+6)%7)+(w-1)*7)
		return monday, monday.AddDate(0, 0, 7), true
	}
	return start, end, false
}

// fileName is the name of a period's segment: <stamp>.log, <stamp>.<n>.log.
func fileName(stamp string, segment int) string {
	if segment == 0 {
		return stamp + ".log"
	}
	return stamp + "." + strconv.Itoa(segment) + ".log"
}

// parseName splits a log file name into its stamp and segment, ok is false for anything
// that isn't a log file.
func parseName(name string) (stamp string, segment int, compressed, ok bool) {
	base, compressed := strings.CutSuffix(name, ".gz")
	base, ok = strings.CutSuffix(base, ".log")
	if !ok {
		return "", 0, false, false
	}
	stamp = base
	if i := strings.LastIndexByte(base, '.'); i >= 0 {
		n, err := strconv.Atoi(base[i+1:])
		if err != nil || n < 1 {
			return "", 0, false, false
		}
		stamp, segment = base[:i], n
	}
	if _, _, ok := parseStamp(stamp); !ok {
		return "", 0, false, false
	}
	return stamp, segment, compressed, true
}

// closedAfter is how long a file must be left alone to be closed: a writer checks the
// period and the size right before it writes, this covers that moment, generously.
const closedAfter = time.Minute

// File is a log file.
type File struct {
	// Name is the file's name in the dir, Path its path.
	Name, Path string
	// Start and End are its period.
	Start, End time.Time
	// Segment is its number in the period, 0 for the first.
	Segment    int
	Compressed bool
	Size       int64
	// ModTime is its last write.
	ModTime time.Time
	// Closed if no mangal process writes to it anymore: its period is over, or a later
	// segment of its period exists and it wasn't written lately.
	Closed bool

	stamp string
}

// Files are the log files of dir, oldest first (by period, then segment). A dir that
// doesn't exist has none.
func Files(dir string) ([]File, error) {
	return listFiles(dir, now())
}

func listFiles(dir string, now time.Time) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []File
	last := map[string]int{}
	for _, e := range entries {
		stamp, segment, compressed, ok := parseName(e.Name())
		if !ok || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			// gone meanwhile (compressed, deleted by another process)
			continue
		}
		start, end, _ := parseStamp(stamp)
		files = append(files, File{
			Name: e.Name(), Path: filepath.Join(dir, e.Name()),
			Start: start, End: end, Segment: segment, Compressed: compressed,
			Size: info.Size(), ModTime: info.ModTime(), stamp: stamp,
		})
		last[stamp] = max(last[stamp], segment)
	}
	slices.SortFunc(files, compareFiles)
	for i := range files {
		f := &files[i]
		over := !now.Before(f.End.Add(closedAfter))
		later := f.Segment < last[f.stamp] && f.ModTime.Before(now.Add(-closedAfter))
		f.Closed = over || later
	}
	return files, nil
}

func compareFiles(a, b File) int {
	if c := a.Start.Compare(b.Start); c != 0 {
		return c
	}
	if a.Segment != b.Segment {
		return a.Segment - b.Segment
	}
	// the same segment twice: the .log being compressed first
	if a.Compressed != b.Compressed {
		if a.Compressed {
			return 1
		}
		return -1
	}
	return strings.Compare(a.Name, b.Name)
}

// lastSegment is the highest segment of the stamp in dir (compressed ones too), 0 if none.
func lastSegment(dir, stamp string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if s, segment, _, ok := parseName(e.Name()); ok && s == stamp {
			n = max(n, segment)
		}
	}
	return n
}

// maxSegments bounds the search of a segment that isn't full (each one is opened).
const maxSegments = 10000

// openSegment opens the first segment of the stamp, from the last one that exists (or
// from), that isn't full. Other processes may append to it too: no O_EXCL, never renamed.
func openSegment(dir, stamp string, from int, maxSize int64) (*os.File, int, error) {
	segment := max(from, lastSegment(dir, stamp))
	for range maxSegments {
		name := filepath.Join(dir, fileName(stamp, segment))
		if _, err := os.Stat(name + ".gz"); err == nil {
			// compressed: it's closed
			segment++
			continue
		}
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
		if err != nil {
			return nil, 0, err
		}
		if maxSize <= 0 || !full(f, maxSize) {
			return f, segment, nil
		}
		f.Close()
		segment++
	}
	return nil, 0, fmt.Errorf("more than %d log files for %s", maxSegments, stamp)
}

// full is whether the file reached maxSize; a stat error says it isn't (the write tells).
func full(f *os.File, maxSize int64) bool {
	info, err := f.Stat()
	return err == nil && info.Size() >= maxSize
}

// tempPrefix starts the temp files of the compression: .<name>.gz.<pid>.tmp.
const tempPrefix = "."

// staleTemp is how old a temp file is when its process surely died.
const staleTemp = time.Hour

// maintain compresses and deletes the closed files of dir as opts says.
func maintain(dir string, now time.Time, opts Options) error {
	var errs []error
	removeStaleTemps(dir, now)
	if opts.Compress {
		files, err := listFiles(dir, now)
		if err != nil {
			return err
		}
		for _, f := range files {
			if f.Closed && !f.Compressed {
				if err := compress(f); err != nil {
					errs = append(errs, fmt.Errorf("compress %s: %w", f.Name, err))
				}
			}
		}
	}
	files, err := listFiles(dir, now)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, f := range expired(files, now, opts) {
		if err := os.Remove(f.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// expired are the closed files retention deletes: the ones of periods over more than
// Keep days before now's day, then the oldest while there are more than MaxFiles or they
// take more than MaxTotalSize.
func expired(files []File, now time.Time, opts Options) []File {
	var drop []File
	keep := files[:0:0]
	if opts.Keep > 0 {
		y, m, d := now.Date()
		oldest := time.Date(y, m, d-opts.Keep+1, 0, 0, 0, 0, now.Location())
		for _, f := range files {
			if f.Closed && !f.End.After(oldest) {
				drop = append(drop, f)
			} else {
				keep = append(keep, f)
			}
		}
	} else {
		keep = slices.Clone(files)
	}
	var total int64
	for _, f := range keep {
		total += f.Size
	}
	count := len(keep)
	for _, f := range keep {
		over := (opts.MaxFiles > 0 && count > opts.MaxFiles) || (opts.MaxTotalSize > 0 && total > opts.MaxTotalSize)
		if !over {
			break
		}
		if !f.Closed {
			continue
		}
		drop = append(drop, f)
		count--
		total -= f.Size
	}
	return drop
}

// compress writes f gzipped next to it (.gz, its modification time kept) and removes it.
// Another process may compress it at the same time: each writes its own temp file, the
// renames give the same content.
func compress(f File) error {
	src, err := os.Open(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer src.Close()
	dir := filepath.Dir(f.Path)
	tmp := filepath.Join(dir, tempPrefix+f.Name+".gz."+strconv.Itoa(os.Getpid())+".tmp")
	dst, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(dst)
	zw.Name = f.Name
	zw.ModTime = f.ModTime
	_, err = io.Copy(zw, src)
	err = errors.Join(err, zw.Close(), dst.Sync(), dst.Close())
	if err == nil {
		err = os.Chtimes(tmp, f.ModTime, f.ModTime)
	}
	if err == nil {
		err = os.Rename(tmp, f.Path+".gz")
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	src.Close()
	if err := os.Remove(f.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// removeStaleTemps deletes the temp files of compressions that never finished.
func removeStaleTemps(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, tempPrefix) || !strings.HasSuffix(name, ".tmp") || !strings.Contains(name, ".log.gz.") {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(now.Add(-staleTemp)) {
			os.Remove(filepath.Join(dir, name))
		}
	}
}
