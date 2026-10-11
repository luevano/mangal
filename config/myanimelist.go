package config

import "strings"

type configMyAnimeList struct {
	ClientID     *entry[string, string]
	ClientSecret *entry[string, string]
	Enabled      *entry[bool, bool]
	// History is read.history.myanimelist.
	History *entry[bool, bool]
}

// MyAnimeList are the metadata.myanimelist keys and read.history.myanimelist.
var MyAnimeList = configMyAnimeList{
	ClientID: reg(entry[string, string]{
		Key: "metadata.myanimelist.client_id",
		Description: "Client ID of a MyAnimeList API client, empty disables MyAnimeList. Register one at https://myanimelist.net/apiconfig " +
			`(App Type "other", App Redirect URL "http://localhost:6969/oauth/mal/callback"), the ID is on its page.`,
		Unmarshal: func(raw string) (string, error) { return strings.TrimSpace(raw), nil },
		Marshal:   func(value string) (string, error) { return value, nil },
	}),
	ClientSecret: reg(entry[string, string]{
		Key:         "metadata.myanimelist.client_secret",
		Description: `Client secret of the MyAnimeList API client, only for App Types other than "other" (those have none).`,
		Secret:      true,
		Unmarshal:   func(raw string) (string, error) { return strings.TrimSpace(raw), nil },
		Marshal:     func(value string) (string, error) { return value, nil },
	}),
	Enabled: boolEntry("metadata.myanimelist.enabled", true,
		"Search MyAnimeList for metadata when Anilist and Kitsu find none (or fail). Only with a metadata.myanimelist.client_id."),
	History: boolEntry("read.history.myanimelist", false,
		"Sync to MyAnimeList reading history if logged in (mangal myanimelist)."),
}

// Configured is true when MyAnimeList can be used: enabled, with a client id.
func (c configMyAnimeList) Configured() bool {
	return c.Enabled.Get() && c.ClientID.Get() != ""
}
