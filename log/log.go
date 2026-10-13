// Package log is mangal's log: a file per day in the state dir and the recent lines for the
// TUI (ctrl+l). Lines logged before Init only go to the recent lines.
package log

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	default:
		return "ERROR"
	}
}

// Options of Init.
type Options struct {
	// Dir of the log files, none are written if empty.
	Dir string
	// Level is the lowest level logged.
	Level Level
	// Keep is how many days of files are kept (today's included), 0 for all.
	Keep int
}

// maxLines is how many recent lines are kept for Lines.
const maxLines = 1000

var (
	mu      sync.Mutex
	options = Options{Level: LevelInfo}
	file    *os.File
	// fileDay is the date of file, a line of another day opens that day's
	fileDay string
	// fileErr disables the file after it couldn't be opened
	fileErr error
	ring    [maxLines]string
	next    int
	full    bool

	// now is swapped in tests
	now = time.Now
)

// Init sets where and what to log. The file is created on the first line, in Dir
// (0700) as <date>.log (0600); a line of the next day goes to that day's file (a long
// mangal serve), and the files older than Keep days are deleted then.
func Init(opts Options) {
	mu.Lock()
	defer mu.Unlock()
	if file != nil {
		file.Close()
		file = nil
	}
	fileErr = nil
	options = opts
}

// Close closes the log file, if open.
func Close() error {
	mu.Lock()
	defer mu.Unlock()
	if file == nil {
		return nil
	}
	err := file.Close()
	file = nil
	return err
}

func Debug(format string, a ...any) { logf(LevelDebug, format, a...) }
func Info(format string, a ...any)  { logf(LevelInfo, format, a...) }
func Warn(format string, a ...any)  { logf(LevelWarn, format, a...) }
func Error(format string, a ...any) { logf(LevelError, format, a...) }

func logf(level Level, format string, a ...any) {
	msg := strings.TrimRight(fmt.Sprintf(format, a...), "\n")
	now := now()

	mu.Lock()
	defer mu.Unlock()
	if level < options.Level {
		return
	}

	prefix := ""
	if level != LevelInfo {
		prefix = level.String() + " "
	}
	ring[next] = fmt.Sprintf("[%s] %s%s", now.Format(time.TimeOnly), prefix, msg)
	next = (next + 1) % maxLines
	if next == 0 {
		full = true
	}

	if f := openFile(now); f != nil {
		fmt.Fprintf(f, "%s %-5s %s\n", now.Format("2006-01-02T15:04:05.000"), level, msg)
	}
}

// openFile returns the log file of now's day, opening it on its first line. Called with mu
// held.
func openFile(now time.Time) *os.File {
	day := now.Format(time.DateOnly)
	if file != nil && day != fileDay {
		file.Close()
		file = nil
	}
	if file != nil || fileErr != nil || options.Dir == "" {
		return file
	}
	if err := os.MkdirAll(options.Dir, 0o700); err != nil {
		fileErr = err
		return nil
	}
	name := filepath.Join(options.Dir, day+".log")
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		fileErr = err
		ringLine(now, "ERROR log file: "+err.Error())
		return nil
	}
	file, fileDay = f, day
	if err := prune(options.Dir, now, options.Keep); err != nil {
		ringLine(now, "ERROR delete old log files: "+err.Error())
	}
	return file
}

// ringLine adds a line about the log itself, so it shows in the TUI logs. Called with mu
// held.
func ringLine(now time.Time, line string) {
	ring[next] = fmt.Sprintf("[%s] %s", now.Format(time.TimeOnly), line)
	next = (next + 1) % maxLines
	if next == 0 {
		full = true
	}
}

// prune deletes the <date>.log files of dir older than keep days before now's (0 keeps
// them all).
func prune(dir string, now time.Time, keep int) error {
	if keep <= 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	y, m, d := now.Date()
	oldest := time.Date(y, m, d-keep+1, 0, 0, 0, 0, time.UTC)
	var errs []error
	for _, e := range entries {
		date, ok := strings.CutSuffix(e.Name(), ".log")
		if !ok || e.IsDir() {
			continue
		}
		t, err := time.Parse(time.DateOnly, date)
		if err != nil || !t.Before(oldest) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Lines are the recent lines, oldest first.
func Lines() string {
	mu.Lock()
	defer mu.Unlock()
	var sb strings.Builder
	start, n := 0, next
	if full {
		start, n = next, maxLines
	}
	for i := range n {
		sb.WriteString(ring[(start+i)%maxLines])
		sb.WriteByte('\n')
	}
	return sb.String()
}

// ProviderHook returns a libmangal Logger hook (SetOnLog) that logs the provider's
// messages, prefixed by name if not empty. The per page messages are Debug.
func ProviderHook(name string) func(format string, a ...any) {
	if name != "" {
		name += ": "
	}
	return func(format string, a ...any) {
		level := LevelInfo
		// libmangal's prefix (e.g. the provider id) comes before the message
		if _, msg, ok := strings.Cut(format, ": "); strings.HasPrefix(format, "page") || (ok && strings.HasPrefix(msg, "page")) {
			level = LevelDebug
		}
		logf(level, "%s%s", name, fmt.Sprintf(format, a...))
	}
}
