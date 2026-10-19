package info

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"text/template"

	"github.com/luevano/libmangal"
	luahttp "github.com/luevano/luaprovider/lib/http"
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
	// RateLimits of a Lua provider's requests, on top of the ones its script sets.
	RateLimits []RateLimit `json:"rate_limits,omitempty"`
}

// RateLimit is a limit on a Lua provider's requests, a rate_limits entry of mangal.toml:
// Rate is "permits/period" ("1/2s", "5/s", "40/1m"), Match the host (and optional path
// prefix) it applies to, every request if empty, Burst how many can go at once (1 if 0).
type RateLimit struct {
	Rate  string `toml:"rate" json:"rate"`
	Match string `toml:"match,omitempty" json:"match,omitempty"`
	Burst int    `toml:"burst,omitempty" json:"burst,omitempty"`
}

// Parse is the luaprovider limit.
func (r RateLimit) Parse() (luahttp.RateLimit, error) {
	limit, err := luahttp.ParseRateLimit(r.Rate)
	if err != nil {
		return luahttp.RateLimit{}, err
	}
	switch {
	case r.Burst < 0:
		return luahttp.RateLimit{}, fmt.Errorf("rate limit %q: burst can't be negative", r.Rate)
	case strings.HasPrefix(r.Match, "/"):
		return luahttp.RateLimit{}, fmt.Errorf("rate limit %q: match %q needs a host before the path", r.Rate, r.Match)
	}
	limit.Match, limit.Burst = r.Match, r.Burst
	return limit, nil
}

// LuaRateLimits are the parsed RateLimits.
func (i Info) LuaRateLimits() ([]luahttp.RateLimit, error) {
	var limits []luahttp.RateLimit
	for n, r := range i.RateLimits {
		limit, err := r.Parse()
		if err != nil {
			return nil, fmt.Errorf("rate_limits[%d]: %w", n, err)
		}
		limits = append(limits, limit)
	}
	return limits, nil
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
	Icon        string   `toml:"icon,omitempty"`
	Mirrors     []string `toml:"mirrors,omitempty"`
	History     []alias  `toml:"history,omitempty"`
	// RateLimits only apply to Lua providers.
	RateLimits []RateLimit `toml:"rate_limits,omitempty"`
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
		if strict, ok := errors.AsType[*toml.StrictMissingError](err); ok {
			keys := make([]string, len(strict.Errors))
			for i, e := range strict.Errors {
				keys[i] = strings.Join(e.Key(), ".")
			}
			return Info{}, fmt.Errorf("unknown keys: %s", strings.Join(keys, ", "))
		}
		return Info{}, err
	}

	info := Info{
		ID:          f.ID,
		Name:        f.Name,
		Version:     f.Version,
		Description: f.Description,
		Website:     f.Website,
		Icon:        f.Icon,
		Mirrors:     f.Mirrors,
		Type:        f.Type,
		RateLimits:  f.RateLimits,
	}
	for _, a := range f.History {
		info.History = append(info.History, libmangal.ProviderAlias(a))
	}
	if len(info.RateLimits) > 0 && info.Type != TypeLua {
		return Info{}, errors.New("rate_limits only apply to lua providers")
	}
	if _, err := info.LuaRateLimits(); err != nil {
		return Info{}, err
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
		Icon:        i.Icon,
		Mirrors:     i.Mirrors,
		RateLimits:  i.RateLimits,
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
