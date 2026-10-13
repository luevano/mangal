package info

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"text/template"

	"github.com/luevano/libmangal"
	"github.com/pelletier/go-toml/v2"
)

//go:generate go tool enumer -type=Type -trimprefix=Type -json -text
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

// file is Info as it's written in mangal.toml: go-toml v2 only reads toml
// tags and libmangal's ProviderInfo has json ones.
type file struct {
	Type        Type     `toml:"type"`
	ID          string   `toml:"id"`
	Name        string   `toml:"name"`
	Version     string   `toml:"version"`
	Description string   `toml:"description"`
	Website     string   `toml:"website"`
	Mirrors     []string `toml:"mirrors,omitempty"`
	History     []alias  `toml:"history,omitempty"`
}

type alias struct {
	ID      string `toml:"id"`
	Name    string `toml:"name,omitempty"`
	Website string `toml:"website,omitempty"`
}

// New parses info from reader. Unknown keys are an error.
func New(r io.Reader) (Info, error) {
	decoder := toml.NewDecoder(r)
	decoder.DisallowUnknownFields()

	var f file
	if err := decoder.Decode(&f); err != nil {
		var strict *toml.StrictMissingError
		if errors.As(err, &strict) {
			keys := make([]string, len(strict.Errors))
			for i, e := range strict.Errors {
				keys[i] = strings.Join(e.Key(), ".")
			}
			return Info{}, fmt.Errorf("unknown keys: %s", strings.Join(keys, ", "))
		}
		return Info{}, err
	}

	info := Info{
		ProviderInfo: libmangal.ProviderInfo{
			ID:          f.ID,
			Name:        f.Name,
			Version:     f.Version,
			Description: f.Description,
			Website:     f.Website,
			Mirrors:     f.Mirrors,
		},
		Type: f.Type,
	}
	for _, a := range f.History {
		info.History = append(info.History, libmangal.ProviderAlias(a))
	}
	return info, nil
}

// Encode writes the info as mangal.toml.
func (i Info) Encode(w io.Writer) error {
	f := file{
		Type:        i.Type,
		ID:          i.ID,
		Name:        i.Name,
		Version:     i.Version,
		Description: i.Description,
		Website:     i.Website,
		Mirrors:     i.Mirrors,
	}
	for _, a := range i.History {
		f.History = append(f.History, alias(a))
	}
	return toml.NewEncoder(w).Encode(f)
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
