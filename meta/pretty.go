package meta

import (
	"log"
	"runtime/debug"
	"strings"
	"text/template"

	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/theme/style"
)

type versioned struct {
	Version string
}

type providers struct {
	Mango versioned
	Lua   versioned
}

type versionInfo struct {
	Mangal    versioned
	Libmangal versioned
	Providers providers
}

func getVersionInfo() (info versionInfo) {
	info.Mangal.Version = Version

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	for _, dep := range bi.Deps {
		switch dep.Path {
		case "github.com/luevano/libmangal":
			info.Libmangal.Version = dep.Version
		case "github.com/luevano/mangoprovider":
			info.Providers.Mango.Version = dep.Version
		case "github.com/luevano/luaprovider":
			info.Providers.Lua.Version = dep.Version
		}
	}

	return info
}

func PrettyVersion() string {
	var info strings.Builder
	err := template.Must(template.New("version").Parse(`
mangal {{ .Mangal.Version }}
libmangal {{ .Libmangal.Version }}
mangoprovider {{ .Providers.Mango.Version }}
luaprovider {{ .Providers.Lua.Version }}

https://github.com/luevano/mangal
`)).Execute(&info, getVersionInfo())
	if err != nil {
		log.Fatal(err)
	}

	return lipgloss.JoinVertical(
		lipgloss.Center,
		style.Bold.Accent.Render(Logo),
		// strings.Repeat("  \n", lipgloss.Height(Logo)),
		info.String(),
	)
}
