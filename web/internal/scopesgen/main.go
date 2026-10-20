// Command scopesgen writes the web UI's api/scopes.json from the API spec.
package main

import (
	"fmt"
	"os"

	"github.com/luevano/mangal/v5/web"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: scopesgen <scopes.json>")
		os.Exit(2)
	}
	b, err := web.UIScopesJSON()
	if err == nil {
		err = os.WriteFile(os.Args[1], b, 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
