package funcs

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"text/template"

	"github.com/luevano/mangal/v5/util/string/path"
	"github.com/spf13/viper"
)

type TemplateFunc struct {
	Value       any
	Description string
}

var FuncMap = newFuncMap()

var Funcs = map[string]TemplateFunc{
	"sanitizeUNIX": {
		Value:       path.SanitizeUNIX,
		Description: fmt.Sprintf("Remove invalid UNIX path chars (%s).", path.InvalidPathCharsUNIX),
	},
	"sanitizeDarwin": {
		Value:       path.SanitizeDarwin,
		Description: fmt.Sprintf("Remove invalid Darwin path chars (%s).", path.InvalidPathCharsDarwin),
	},
	"sanitizeWindows": {
		Value:       path.SanitizeWindows,
		Description: fmt.Sprintf("Remove invalid Windows path chars (%s).", path.InvalidPathCharsWindows),
	},
	"sanitize": {
		Value:       path.SanitizeOS,
		Description: fmt.Sprintf("Remove invalid path chars (%s) on host OS (%s).", path.InvalidPathCharsOS, runtime.GOOS),
	},
	"sanitizeWhitespace": {
		Value:       path.SanitizeWhitespace,
		Description: "Replace all whitespace (as defined by Unicode's White Space property) chars with underscores.",
	},
	"ceil": {
		Value:       math.Ceil,
		Description: "Returns the least integer value greater than or equal to x.",
	},
	"floor": {
		Value:       math.Floor,
		Description: "Returns the greatest integer value less than or equal to x.",
	},
	"replaceAll": {
		Value:       strings.ReplaceAll,
		Description: "Returns a copy of the string s with all non-overlapping instances of old replaced by new.",
	},
	"replace": {
		Value:       strings.Replace,
		Description: "Returns a copy of the string s with the first n non-overlapping instances of old replaced by new.",
	},
	"upper": {
		Value:       strings.ToUpper,
		Description: "Returns s with all Unicode letters mapped to their upper case.",
	},
	"lower": {
		Value:       strings.ToLower,
		Description: "Returns s with all Unicode letters mapped to their lower case.",
	},
	"title": {
		Value:       strings.ToTitle,
		Description: "Returns s with all Unicode letters mapped to their Unicode title case.",
	},
	"getConfig": {
		Value:       viper.Get,
		Description: "Returns the config associated with the given key.",
	},
}

func newFuncMap() template.FuncMap {
	m := make(template.FuncMap)

	for k, f := range Funcs {
		m[k] = f.Value
	}

	return m
}
