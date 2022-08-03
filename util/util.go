package util

import (
	"fmt"
	"github.com/samber/lo"
	"regexp"
	"strings"
)

func PrettyTrim(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	return s[:maxLen] + "..."
}

func PadZero(s string, l int) string {
	for l > len(s) {
		s = "0" + s
	}

	return s
}

// replacers is a list of regexp.Regexp pairs that will be used to sanitize filenames.
var replacers = []lo.Tuple2[*regexp.Regexp, string]{
	{regexp.MustCompile(`[\\/<>:"|?*\s]`), "_"},
	{regexp.MustCompile(`__+`), "_"},
	{regexp.MustCompile(`^_+|_+$`), ""},
	{regexp.MustCompile(`^\.+|\.+$`), ""},
}

// SanitizeFilename will remove all invalid characters from a path.
func SanitizeFilename(filename string) string {
	for _, re := range replacers {
		filename = re.A.ReplaceAllString(filename, re.B)
	}

	return filename
}

func Quantity(count int, thing string) string {
	if strings.HasSuffix(thing, "s") {
		thing = thing[:len(thing)-1]
	}

	if count == 1 {
		return fmt.Sprintf("%d %s", count, thing)
	}

	return fmt.Sprintf("%d %ss", count, thing)
}
