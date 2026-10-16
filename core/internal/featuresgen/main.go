// Command featuresgen writes the web UI's features.json from core.Features, or with -docs
// the mode matrix into the workspace's docs/mangal.md (found above the module, between
// core's markers; skipped when there's none).
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/luevano/mangal/v5/core"
)

func main() {
	var err error
	switch {
	case len(os.Args) == 2 && os.Args[1] == "-docs":
		err = docs("")
	case len(os.Args) == 3 && os.Args[1] == "-docs":
		err = docs(os.Args[2])
	case len(os.Args) == 2:
		var b []byte
		if b, err = core.WebFeaturesJSON(); err == nil {
			err = os.WriteFile(os.Args[1], b, 0o644)
		}
	default:
		fmt.Fprintln(os.Stderr, "usage: featuresgen <features.json> | -docs [mangal.md]")
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// docs writes the matrix into path, the first docs/mangal.md above the working directory
// when it's "".
func docs(path string) error {
	if path == "" {
		dir, err := os.Getwd()
		if err != nil {
			return err
		}
		for {
			p := filepath.Join(dir, "docs", "mangal.md")
			if _, err := os.Stat(p); err == nil {
				path = p
				break
			} else if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				fmt.Println("featuresgen: no docs/mangal.md above, the mode matrix isn't written")
				return nil
			}
			dir = parent
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	doc, ok := core.ReplaceFeatures(string(b))
	if !ok {
		return fmt.Errorf("%s has no %s ... %s", path, core.FeaturesBegin, core.FeaturesEnd)
	}
	return os.WriteFile(path, []byte(doc), 0o644)
}
