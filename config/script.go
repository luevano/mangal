package config

// Script are the scripts' settings.
var Script = configScript{
	APIScopes: func() *entry[string, []string] {
		e := listEntry("script.api_scopes",
			"The permissions (as a user's: read, progress, browse, download, manage, tokens, admin) of mangal.api and `mangal api --local` when no mangal serve runs: the in-process API. `mangal script --write` and `mangal api --local --write` add download and manage for one run.")
		e.Default = []string{"read", "progress", "browse"}
		return e
	}(),
}

type configScript struct {
	APIScopes *entry[string, []string]
}
