//go:build !webui

package web

import (
	"embed"
	"io/fs"
)

// without the webui tag (e.g. go install) there's no built UI to embed
//
//go:embed noui
var noUI embed.FS

func frontend() (fs.FS, error) {
	return fs.Sub(noUI, "noui")
}
