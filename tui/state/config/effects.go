package config

import "strings"

// effect is when a change of a key applies, most apply at once: they're read when used.
type effect int

const (
	applyNow effect = iota
	// the providers keep the options they were loaded with
	applyReload
	// read once at startup
	applyRestart
	// captured by the chapter lists when they open
	applyNewScreens
	applyCLI
	applyIcons
)

// effects by key, or by prefix for the ones ending in a dot.
var effects = []struct {
	key    string
	effect effect
}{
	{"icons", applyIcons},
	{"providers.", applyReload},
	{"metadata.", applyReload},
	{"download.user_agent", applyReload},
	{"download.mode_dir_decimal", applyReload},
	{"download.mode_file_decimal", applyReload},
	{"cache.", applyRestart},
	{"auth.keyring", applyRestart},
	{"tui.skip_home", applyRestart},
	{"tui.chapter.", applyNewScreens},
	{"cli.", applyCLI},
}

func effectOf(key string) effect {
	for _, e := range effects {
		if key == e.key || (strings.HasSuffix(e.key, ".") && strings.HasPrefix(key, e.key)) {
			return e.effect
		}
	}
	return applyNow
}

func (e effect) hint() string {
	switch e {
	case applyReload:
		return "applies to the providers loaded after this (the providers list reloads them)"
	case applyRestart:
		return "applies the next time mangal starts"
	case applyNewScreens:
		return "applies to the chapter lists opened after this"
	case applyCLI:
		return "only for the CLI"
	case applyIcons:
		return "some screens show the new icons once reopened"
	default:
		return ""
	}
}
