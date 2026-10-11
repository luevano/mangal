package meta

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// versioned are the modules shown by Versions, in order.
var versioned = []struct{ name, path string }{
	{"libmangal", "github.com/luevano/libmangal"},
	{"mangoprovider", "github.com/luevano/mangoprovider"},
	{"mangodex", "github.com/luevano/mangodex"},
	{"mangoplus", "github.com/luevano/mangoplus"},
	{"luaprovider", "github.com/luevano/luaprovider"},
}

// Versions lists the version of mangal and of its main libraries, one per line.
func Versions() string {
	deps := map[string]string{}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range bi.Deps {
			version := dep.Version
			if dep.Replace != nil {
				version = dep.Replace.Version
			}
			deps[dep.Path] = version
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s\n", AppName, Version)
	for _, m := range versioned {
		fmt.Fprintf(&sb, "%s %s\n", m.name, deps[m.path])
	}
	sb.WriteString("\nhttps://github.com/luevano/mangal\n")
	return sb.String()
}
