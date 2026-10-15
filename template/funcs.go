package template

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"text/template"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/util/string/path"
	"github.com/spf13/cast"
)

// Func is a function available in the name templates.
type Func struct {
	Name        string
	Value       any
	Description string
}

// Funcs are the functions available in the name templates, sorted by name.
var Funcs = []Func{
	{"ceil", ceil, "Returns the least integer value greater than or equal to x."},
	{"floor", floor, "Returns the greatest integer value less than or equal to x."},
	{"getConfig", getConfig, "Returns the value of the given config key (as in the config file). Secret keys (tokens, webhooks) aren't available."},
	{"lower", strings.ToLower, "Returns s with all Unicode letters mapped to their lower case."},
	{"replace", strings.Replace, "Returns a copy of the string s with the first n non-overlapping instances of old replaced by new."},
	{"replaceAll", strings.ReplaceAll, "Returns a copy of the string s with all non-overlapping instances of old replaced by new."},
	{"sanitize", path.SanitizeOS, fmt.Sprintf("Remove invalid path chars (%s) on host OS (%s), and control chars; caps the length and avoids empty, . and .. names.", libmangal.InvalidPathCharsOS, runtime.GOOS)},
	{"sanitizeDarwin", path.SanitizeDarwin, fmt.Sprintf("Remove invalid Darwin path chars (%s), as sanitize.", libmangal.InvalidPathCharsDarwin)},
	{"sanitizeUNIX", path.SanitizeUNIX, fmt.Sprintf("Remove invalid UNIX path chars (%s), as sanitize.", libmangal.InvalidPathCharsUNIX)},
	{"sanitizeWhitespace", path.SanitizeWhitespace, "Replace all whitespace (as defined by Unicode's White Space property) chars with underscores."},
	{"sanitizeWindows", path.SanitizeWindows, fmt.Sprintf("Remove invalid Windows path chars (%s), as sanitize, and prefix reserved names (CON, NUL, ...).", libmangal.InvalidPathCharsWindows)},
	// strings.ToTitle is the Unicode title case of each letter, the same as upper for most scripts
	{"title", strings.ToTitle, `Returns s with all letters in Unicode title case, which for most scripts is upper case ("one piece" gives "ONE PIECE"); it doesn't capitalize words.`},
	{"upper", strings.ToUpper, "Returns s with all Unicode letters mapped to their upper case."},
}

var funcMap = func() template.FuncMap {
	m := make(template.FuncMap, len(Funcs))
	for _, f := range Funcs {
		m[f.Name] = f.Value
	}
	return m
}()

// numbers can be float32 (chapter and volume numbers), float64 or ints
func ceil(x any) (float64, error) {
	f, err := cast.ToFloat64E(x)
	return math.Ceil(f), err
}

func floor(x any) (float64, error) {
	f, err := cast.ToFloat64E(x)
	return math.Floor(f), err
}

// getConfig gives nil for unknown keys, as before.
func getConfig(key string) (any, error) {
	if !config.Exists(key) {
		return nil, nil
	}
	if config.IsSecret(key) {
		return nil, fmt.Errorf("config key %q is secret", key)
	}
	return config.Get(key)
}
