// Package log is mangal's log: rotated files in the state dir (several processes write
// them at once, see Options), the recent lines for the TUI and reading the files back
// (Files, Scan, Tail). Lines logged before Init only go to the recent lines.
package log

import (
	"fmt"
	"os"
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

// ParseLevel is the Level of its name, any case.
func ParseLevel(s string) (Level, error) {
	for _, l := range []Level{LevelDebug, LevelInfo, LevelWarn, LevelError} {
		if strings.EqualFold(s, l.String()) {
			return l, nil
		}
	}
	return LevelInfo, fmt.Errorf("%q isn't a level (debug, info, warn, error)", s)
}

// Options of Init.
type Options struct {
	// Dir of the log files, none are written if empty.
	Dir string
	// Level is the lowest level logged.
	Level Level
	// Rotate is the period of a file.
	Rotate Period
	// MaxSize starts the next segment of the period once a file has this many bytes, 0
	// for no limit.
	MaxSize int64
	// Keep is how many days of files are kept (today's included), 0 for all.
	Keep int
	// MaxFiles and MaxTotalSize delete the oldest closed files beyond them, 0 for no limit.
	MaxFiles     int
	MaxTotalSize int64
	// Compress gzips the closed files.
	Compress bool
}

// maxLines is how many recent lines are kept for Lines.
const maxLines = 1000

var (
	mu      sync.Mutex
	options = Options{Level: LevelInfo}
	file    *os.File
	// fileStamp and fileSegment are file's period and segment, a line of another period
	// opens that one's
	fileStamp   string
	fileSegment int
	// fileErr disables the file after it couldn't be opened
	fileErr  error
	ring     [maxLines]Entry
	next     int
	ringFull bool

	// maintaining is set while maintenance runs, again asks for another run after it
	maintaining, again bool
	againAt            time.Time
	maintenance        sync.WaitGroup

	// now is swapped in tests
	now = time.Now
)

// Init sets where and what to log. The file is created on the first line, in Dir (0700),
// as <period>.log (0600), see Options; a line of the next period goes to that period's file
// (a long mangal serve), and the closed files are compressed and deleted (as Options say)
// in the background when a new one is opened.
func Init(opts Options) {
	mu.Lock()
	closeFile()
	fileErr = nil
	options = opts
	mu.Unlock()
	maintenance.Wait()
}

// Close closes the log file, if open, and waits for the maintenance.
func Close() error {
	mu.Lock()
	err := closeFile()
	mu.Unlock()
	maintenance.Wait()
	return err
}

// closeFile is called with mu held.
func closeFile() error {
	if file == nil {
		return nil
	}
	err := file.Close()
	file = nil
	return err
}

// Dir is the dir of the log files, empty if none.
func Dir() string {
	mu.Lock()
	defer mu.Unlock()
	return options.Dir
}

// FileError is why the log file isn't written (it couldn't be opened), nil if it is or
// will be at the next line.
func FileError() error {
	mu.Lock()
	defer mu.Unlock()
	return fileErr
}

// Current is the path of the file this process writes, empty if none yet.
func Current() string {
	mu.Lock()
	defer mu.Unlock()
	if file == nil {
		return ""
	}
	return file.Name()
}

// Enabled: Whether lines of level are logged.
func Enabled(level Level) bool {
	mu.Lock()
	defer mu.Unlock()
	return level >= options.Level
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

	ringAdd(Entry{Time: now, Level: level, Message: msg})

	if f := openFile(now); f != nil {
		fmt.Fprintf(f, "%s %-5s %s\n", now.Format("2006-01-02T15:04:05.000"), level, msg)
	}
}

// openFile returns the file to write a line of now to: the one of now's period, the next
// segment when it's full. Called with mu held.
func openFile(now time.Time) *os.File {
	if options.Dir == "" || fileErr != nil {
		return nil
	}
	stamp := options.Rotate.stamp(now)
	from := 0
	if file != nil && stamp != fileStamp {
		closeFile()
	}
	if file != nil && options.MaxSize > 0 && full(file, options.MaxSize) {
		from = fileSegment + 1
		closeFile()
	}
	if file != nil {
		return file
	}
	if err := os.MkdirAll(options.Dir, 0o700); err != nil {
		fileErr = err
		return nil
	}
	f, segment, err := openSegment(options.Dir, stamp, from, options.MaxSize)
	if err != nil {
		fileErr = err
		ringAdd(Entry{Time: now, Level: LevelError, Message: "log file: " + err.Error()})
		return nil
	}
	file, fileStamp, fileSegment = f, stamp, segment
	startMaintenance(now)
	return file
}

// startMaintenance compresses and deletes the closed files (as of at) in the background,
// once at a time. Called with mu held.
func startMaintenance(at time.Time) {
	if maintaining {
		again, againAt = true, at
		return
	}
	maintaining = true
	opts := options
	maintenance.Go(func() {
		for {
			err := maintain(opts.Dir, at, opts)
			mu.Lock()
			if err != nil {
				ringAdd(Entry{Time: at, Level: LevelError, Message: "log files: " + err.Error()})
			}
			if !again {
				maintaining = false
				mu.Unlock()
				return
			}
			again, at, opts = false, againAt, options
			mu.Unlock()
		}
	})
}

// ringAdd adds a recent entry (also the log's own errors, so they show in the TUI). Called
// with mu held.
func ringAdd(e Entry) {
	ring[next] = e
	next = (next + 1) % maxLines
	if next == 0 {
		ringFull = true
	}
}

// Recent are the entries logged lately by this process (files or not), oldest first.
func Recent() []Entry {
	mu.Lock()
	defer mu.Unlock()
	start, n := 0, next
	if ringFull {
		start, n = next, maxLines
	}
	entries := make([]Entry, n)
	for i := range n {
		entries[i] = ring[(start+i)%maxLines]
	}
	return entries
}

// Lines are the recent entries as lines, oldest first.
func Lines() string {
	var sb strings.Builder
	for _, e := range Recent() {
		sb.WriteString("[" + e.Time.Format(time.TimeOnly) + "] ")
		if e.Level != LevelInfo {
			sb.WriteString(e.Level.String() + " ")
		}
		sb.WriteString(e.Message)
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
