//go:build webui

package web

import (
	"embed"
	"io/fs"
)

// the UI is built in web/ui (npm run build), it isn't committed
//
//go:embed all:ui/dist
var ui embed.FS

func frontend() (fs.FS, error) {
	return fs.Sub(ui, "ui/dist")
}
