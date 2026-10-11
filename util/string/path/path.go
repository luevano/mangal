package path

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	InvalidPathCharsUNIX    = `/`
	InvalidPathCharsDarwin  = `/:`
	InvalidPathCharsWindows = `<>:"/\|?*`
)

// maxNameBytes leaves room for an extension under the usual 255 bytes limit, as libmangal.
const maxNameBytes = 240

var windowsReserved = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

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

// sanitizeName makes name usable as a single file or directory name: invalid and control
// chars become underscores, it's capped to maxNameBytes, and it can't be empty, "." or "..".
func sanitizeName(name, invalid string, windows bool) string {
	name = SanitizePath(name, func(r rune) bool {
		return r < 0x20 || r == 0x7f || strings.ContainsRune(invalid, r)
	})

	if len(name) > maxNameBytes {
		name = name[:maxNameBytes]
		// don't leave half a character
		for !utf8.ValidString(name) {
			name = name[:len(name)-1]
		}
	}

	if windows {
		base, _, _ := strings.Cut(name, ".")
		if windowsReserved[strings.ToUpper(strings.TrimSpace(base))] {
			name = "_" + name
		}
	}

	switch name {
	case "", ".", "..":
		return "_"
	}
	return name
}

func SanitizeWhitespace(path string) string {
	return SanitizePath(path, func(r rune) bool {
		return unicode.IsSpace(r)
	})
}

func SanitizeUNIX(path string) string {
	return sanitizeName(path, InvalidPathCharsUNIX, false)
}

func SanitizeDarwin(path string) string {
	return sanitizeName(path, InvalidPathCharsDarwin, false)
}

func SanitizeWindows(path string) string {
	return sanitizeName(path, InvalidPathCharsWindows, true)
}
