package config

import (
	"fmt"
	"strings"
)

// LocalConnection is the connection's value for this machine's own backend.
const LocalConnection = "local"

// Connection is the backend the frontends (TUI, CLI) use. It's not validated at startup:
// mangal serve never reads it, a bad value is the frontends' error when they connect.
var Connection = reg(entry[string, string]{
	Key:         "connection",
	Default:     LocalConnection,
	Description: "The backend the TUI and the CLI use: local (this machine), a name from connections or an address (http://nas.lan:6977). --remote and --local for one run. mangal serve ignores it.",
})

// Connections are the servers by name.
var Connections = reg(entry[map[string]any, map[string]string]{
	Key:         "connections",
	Default:     map[string]string{},
	Description: "The mangal serve servers by name, e.g. nas = 'https://mangal.example.com' (mangal connection add). Their tokens are saved apart, by address (mangal connection login).",
	Unmarshal: func(raw map[string]any) (map[string]string, error) {
		out := make(map[string]string, len(raw))
		for name, v := range raw {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("connection %q: expected an address, got %T", name, v)
			}
			out[name] = s
		}
		return out, nil
	},
	Marshal: func(conns map[string]string) (map[string]any, error) {
		raw := make(map[string]any, len(conns))
		for name, s := range conns {
			raw[name] = s
		}
		return raw, nil
	},
})

// ValidConnectionName says why name can't be a connection's: it must never read as local or
// an address, and the config's keys are lowercase without dots.
func ValidConnectionName(name string) error {
	switch {
	case name == "" || name == LocalConnection:
		return fmt.Errorf("%q can't name a connection", name)
	case strings.ContainsAny(name, ":/. \t") || name != strings.ToLower(name):
		return fmt.Errorf("connection name %q: lowercase, no spaces, dots, colons or slashes", name)
	}
	return nil
}

type configRemote struct {
	// Token is the connection's API token, else the one `mangal connection login` saved.
	Token   *entry[string, string]
	CAFile  *entry[string, string]
	Timeout *entry[string, string]
	// ReadCache is where the chapters fetched to read go, ReadCacheSize its limit.
	ReadCache     *entry[string, string]
	ReadCacheSize *entry[string, int64]
}

var Remote = configRemote{
	Token: reg(entry[string, string]{
		Key:         "remote.token",
		Description: "API token (`mangal token add` on the server) for the connection, only sent there; normally saved with `mangal connection login` instead (MANGAL_REMOTE_TOKEN for one run).",
		Secret:      true,
	}),
	CAFile: pathEntry("remote.ca_file", "",
		"A CA or certificate file (PEM) to trust for the remote server's HTTPS besides the system's (a self-signed or private CA).", false),
	Timeout: durationEntry("remote.timeout", "30s",
		"How long a request to the remote server may take (0 for no limit).", false),
	ReadCache: pathEntry("remote.read_cache", "",
		"Where a remote TUI keeps the chapters it fetches from the server to read (a private folder). Empty: `remote` in the cache directory (cache.path).", false),
	ReadCacheSize: sizeEntry("remote.read_cache_size", "1GB",
		"How much remote.read_cache keeps: the chapters read longest ago are removed past it (0 for no limit)."),
}
