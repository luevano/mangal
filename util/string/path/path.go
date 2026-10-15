package path

import (
	"runtime"
	"strings"
	"unicode"

	"github.com/luevano/libmangal"
)

func SanitizePath(path string, isInvalid func(rune) bool) string {
	var (
		sanitized strings.Builder
		prev      rune
	)

	const underscore = '_'

	for _, r := range path {
		var toWrite rune
		if isInvalid(r) {
			toWrite = underscore
		} else {
			toWrite = r
		}

		// replace two or more consecutive underscores with one underscore
		if (toWrite == underscore && prev != underscore) || toWrite != underscore {
			sanitized.WriteRune(toWrite)
		}

		prev = toWrite
	}

	return strings.TrimFunc(sanitized.String(), func(r rune) bool {
		return r == underscore || unicode.IsSpace(r)
	})
}

// sanitizeName makes name usable as a single file or directory name: libmangal's
// SanitizeName after trimming underscores and spaces from both ends (what mangal always did).
func sanitizeName(name, invalid string, windows bool) string {
	name = SanitizePath(name, func(r rune) bool {
		return r < 0x20 || r == 0x7f || strings.ContainsRune(invalid, r)
	})
	return libmangal.SanitizeName(name, invalid, windows)
}

func SanitizeWhitespace(path string) string {
	return SanitizePath(path, func(r rune) bool {
		return unicode.IsSpace(r)
	})
}

func SanitizeUNIX(path string) string {
	return sanitizeName(path, libmangal.InvalidPathCharsUNIX, false)
}

func SanitizeDarwin(path string) string {
	return sanitizeName(path, libmangal.InvalidPathCharsDarwin, false)
}

// SanitizeOS is SanitizeUNIX, SanitizeDarwin or SanitizeWindows, for this OS.
func SanitizeOS(path string) string {
	return sanitizeName(path, libmangal.InvalidPathCharsOS, runtime.GOOS == "windows")
}

func SanitizeWindows(path string) string {
	return sanitizeName(path, libmangal.InvalidPathCharsWindows, true)
}
