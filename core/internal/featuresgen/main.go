// Command featuresgen writes the web UI's features.json from core.Features.
package main

import (
	"fmt"
	"os"

	"github.com/luevano/mangal/v5/core"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: featuresgen <features.json>")
		os.Exit(2)
	}
	b, err := core.WebFeaturesJSON()
	if err == nil {
		err = os.WriteFile(os.Args[1], b, 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
