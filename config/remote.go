package config

import (
	"fmt"
	"net/url"
)

type configRemote struct {
	// URL is the `mangal serve` the commands on core use, empty for this machine's.
	URL *entry[string, string]
	// Token is remote.url's API token, else the one `mangal remote login` saved.
	Token   *entry[string, string]
	CAFile  *entry[string, string]
	Timeout *entry[string, string]
}

var Remote = configRemote{
	URL: reg(entry[string, string]{
		Key:         "remote.url",
		Default:     "",
		Description: "The `mangal serve` (http://nas.lan:6977, https://mangal.example.com) that the library changes, `library undo`, `health fix` and `history` of the CLI use instead of this machine's library, as `--remote` does (`--local` for this machine's). Its token: remote.token, else the one `mangal remote login` saved. Empty: this machine's.",
		Validate: func(s string) error {
			if s == "" {
				return nil
			}
			u, err := url.Parse(s)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return fmt.Errorf("%q isn't an http or https address", s)
			}
			return nil
		},
	}),
	Token: reg(entry[string, string]{
		Key:         "remote.token",
		Description: "API token (`mangal token add` on the server) for remote.url, only sent there; normally saved with `mangal remote login` instead (MANGAL_REMOTE_TOKEN for one run).",
		Secret:      true,
	}),
	CAFile: pathEntry("remote.ca_file", "",
		"A CA or certificate file (PEM) to trust for the remote server's HTTPS besides the system's (a self-signed or private CA).", false),
	Timeout: durationEntry("remote.timeout", "30s",
		"How long a request to the remote server may take (0 for no limit).", false),
}
