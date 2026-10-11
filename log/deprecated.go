package log

import (
	"strings"

	"github.com/rs/zerolog"
)

// Log logs at Info.
//
// Deprecated: use Info (or Debug, Warn, Error).
func Log(format string, a ...any) {
	Info(format, a...)
}

type aggregate struct{}

// String returns Lines.
func (aggregate) String() string {
	return Lines()
}

// Aggregate is the recent lines.
//
// Deprecated: use Lines.
var Aggregate aggregate

// L logs into this package's log.
//
// Deprecated: use Debug, Info, Warn or Error.
var L = func() *zerolog.Logger {
	l := zerolog.New(zerolog.ConsoleWriter{
		Out:             zerologWriter{},
		NoColor:         true,
		PartsOrder:      []string{zerolog.LevelFieldName, zerolog.MessageFieldName},
		FormatTimestamp: func(any) string { return "" },
	})
	return &l
}()

type zerologWriter struct{}

func (zerologWriter) Write(p []byte) (int, error) {
	line := strings.TrimSpace(string(p))
	level, msg, _ := strings.Cut(line, " ")
	switch level {
	case "DBG", "TRC":
		Debug("%s", msg)
	case "WRN":
		Warn("%s", msg)
	case "ERR", "FTL", "PNC":
		Error("%s", msg)
	case "INF":
		Info("%s", msg)
	default:
		Info("%s", line)
	}
	return len(p), nil
}
