package log

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/rs/zerolog"
	"github.com/samber/lo"
)

var L = lo.Must(newLogger())

func newLogger() (*zerolog.Logger, error) {
	today := time.Now().Format(time.DateOnly)

	logPath := filepath.Join(path.LogDir(), fmt.Sprint(today, ".log"))

	file, err := afs.Afero.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, config.Download.ModeFile.Get())
	if err != nil {
		return nil, err
	}

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	logger := zerolog.New(file)

	return &logger, nil
}
