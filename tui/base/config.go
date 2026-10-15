package base

import (
	"errors"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
)

// NewConfigWindow makes the config window (set by the tui package, the window needs
// base), filtered to filter if not empty. Nil disables the config key.
var NewConfigWindow func(filter string) State

// ConfigWindow is implemented by the config window, the config key doesn't open another.
type ConfigWindow interface {
	State
	// Filter shows only the keys matching filter.
	Filter(filter string) tea.Cmd
}

func (m *model) openConfig(filter string) tea.Cmd {
	if NewConfigWindow == nil || m.standalone {
		return nil
	}
	var cmd tea.Cmd
	if m.inViewport {
		cmd = m.hideViewport()
	}
	if w, ok := m.state.(ConfigWindow); ok {
		return tea.Batch(cmd, w.Filter(filter))
	}
	return tea.Batch(cmd, m.pushState(NewConfigWindow(filter)))
}

// configKeyError is an error that a config key can fix.
type configKeyError struct {
	error
	key string
}

func (e *configKeyError) Unwrap() error { return e.error }

// ConfigError is err fixed by changing the config key: when the error is shown, a key
// opens the config window on it.
func ConfigError(err error, key string) error {
	return &configKeyError{error: err, key: key}
}

func configKeyOf(err error) string {
	var e *configKeyError
	if errors.As(err, &e) {
		return e.key
	}
	return ""
}

var providersStale atomic.Bool

// ReloadProviders tells that the loaded providers have stale options (the config
// changed): the providers list reloads them when it's shown again.
func ReloadProviders() {
	providersStale.Store(true)
}

// TakeReloadProviders is true once after ReloadProviders.
func TakeReloadProviders() bool {
	return providersStale.Swap(false)
}
