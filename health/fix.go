package health

import (
	"errors"
	"fmt"
	"strings"
)

// FixResult is what a finding's fix would do (the plan), or did.
type FixResult struct {
	Key  string `json:"key"`
	Kind string `json:"kind"`
	// Plan are the changes, one per line.
	Plan []string `json:"plan"`
	// Nothing is true when there's nothing to do any more.
	Nothing bool   `json:"nothing,omitempty"`
	Applied bool   `json:"applied"`
	Message string `json:"message,omitempty"`
	// Snapshot is the database snapshot of an applied library change: "mangal library
	// undo" reverts it.
	Snapshot string `json:"snapshot,omitempty"`
}

// NotFixableError is a finding without a fix to apply as a change (core.HealthFixReq):
// its commands fix it (a scan has no plan).
type NotFixableError struct {
	Kind     string
	Commands []string
}

func (e *NotFixableError) Error() string {
	if e.Kind == "" {
		return "the finding has no fix to apply here: " + commandsOr(e.Commands)
	}
	return fmt.Sprintf("a %s isn't applied here: %s", e.Kind, commandsOr(e.Commands))
}

func commandsOr(commands []string) string {
	if len(commands) == 0 {
		return "nothing to run"
	}
	return "run " + strings.Join(commands, " or ")
}

// ErrChoice is a choice missing or not one of the fix's.
var ErrChoice = errors.New("invalid choice")

// Find is the finding (or summary item) with the key.
func (r Report) Find(key string) (Finding, bool) {
	return findKey(r.Findings, key)
}

func findKey(findings []Finding, key string) (Finding, bool) {
	for _, f := range findings {
		if strings.EqualFold(f.Key, key) {
			return f, true
		}
		if item, ok := findKey(f.Items, key); ok {
			return item, true
		}
	}
	return Finding{}, false
}
