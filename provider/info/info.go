package info

import (
	"fmt"
	"io"
	"net/url"
	"strings"
	"text/template"

	"github.com/luevano/libmangal"
	"github.com/pelletier/go-toml"
)

//go:generate enumer -type=Type -trimprefix=Type -json -text
type Type uint8

const (
	TypeBundle Type = iota + 1
	TypeLua
)

const Filename = "mangal.toml"

// Info contains libmangal info about provider with mangal specific type field
type Info struct {
	libmangal.ProviderInfo
	Type Type `json:"type"`
}

// New parses info from reader
func New(r io.Reader) (info Info, err error) {
	decoder := toml.NewDecoder(r)
	decoder.Strict(true)
	decoder.SetTagName("json")

	err = decoder.Decode(&info)
	return
}

var markdown = template.Must(template.New("markdown").Funcs(template.FuncMap{
	"domain": func(rawURL string) string {
		u, err := url.Parse(rawURL)
		if err != nil || u.Hostname() == "" {
			return rawURL
		}
		return u.Hostname()
	},
}).Parse(`
{{ with .ProviderInfo }}
# {{ .Name }} v{{ .Version }}

{{ if .Website }}
Mangal provider for the [{{ domain .Website }}]({{ .Website }})
{{ end }}

> {{ .Description }}
{{ end }}
`))

// Markdown is the provider's README.
func (i Info) Markdown() (string, error) {
	var sb strings.Builder
	if err := markdown.Execute(&sb, i); err != nil {
		return "", fmt.Errorf("render readme: %w", err)
	}
	return strings.TrimSpace(sb.String()), nil
}
