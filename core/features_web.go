package core

import (
	"encoding/json"
)

//go:generate go run ./internal/featuresgen ../web/ui/src/features.json

// WebFeature is a feature as the web UI's features.json has it.
type WebFeature struct {
	Name string   `json:"name"`
	Web  []string `json:"web,omitempty"`
	API  []string `json:"api,omitempty"`
	// NotApplicable is why the web UI doesn't have it.
	NotApplicable string `json:"not_applicable,omitempty"`
}

// WebFeaturesJSON is web/ui/src/features.json: every feature with its web routes (or why
// it has none) and API operations, and the routes of no feature. The UI's test checks its
// router against it.
func WebFeaturesJSON() ([]byte, error) {
	out := struct {
		Features []WebFeature      `json:"features"`
		Routes   map[string]string `json:"routes"`
	}{Routes: WebRoutes}
	for _, f := range Features {
		out.Features = append(out.Features, WebFeature{Name: f.Name, Web: f.Web, API: f.API, NotApplicable: f.NotApplicable[ModeWeb]})
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
