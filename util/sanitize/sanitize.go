// Package sanitize strips terminal control sequences from text that comes from outside
// (provider titles, scanlation groups, errors), so printing it can't move the cursor,
// rewrite the screen, set the title or write the clipboard (OSC 52).
package sanitize

import (
	"strings"
	"unicode/utf8"
)

const esc = 0x1b

// Text is s without control characters (\n and \t stay), escape sequences, C1 controls
// and invalid UTF-8.
func Text(s string) string {
	return clean(s, false)
}

// Styled is Text keeping the SGR sequences (colors, bold): for a rendered view, its styles
// stay. Text from outside could only restyle itself.
func Styled(s string) string {
	return clean(s, true)
}

func clean(s string, keepSGR bool) string {
	if safe(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\n' || c == '\t':
			b.WriteByte(c)
			i++
		case c == esc:
			n := escLen(s[i:])
			if keepSGR && isSGR(s[i:i+n]) {
				b.WriteString(s[i : i+n])
			}
			i += n
		case c < 0x20 || c == 0x7f:
			i++
		case c < utf8.RuneSelf:
			b.WriteByte(c)
			i++
		default:
			r, size := utf8.DecodeRuneInString(s[i:])
			// an invalid byte, or a C1 control (U+0080-U+009F: CSI, OSC... for some
			// terminals), goes; what followed an introducer stays as plain text
			if r != utf8.RuneError && !isC1(r) || size > 1 && r == utf8.RuneError {
				b.WriteString(s[i : i+size])
			}
			i += size
		}
	}
	return b.String()
}

// safe is true when there's nothing to strip, the common case.
func safe(s string) bool {
	ascii := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c < 0x20 && c != '\n' && c != '\t', c == 0x7f:
			return false
		case c == 0xc2 && i+1 < len(s) && s[i+1] >= 0x80 && s[i+1] <= 0x9f:
			return false
		case c >= utf8.RuneSelf:
			ascii = false
		}
	}
	return ascii || utf8.ValidString(s)
}

func isC1(r rune) bool { return r >= 0x80 && r <= 0x9f }

// escLen is the length of the escape sequence s starts with (s[0] is ESC). An unfinished
// one runs to the end.
func escLen(s string) int {
	if len(s) < 2 {
		return len(s)
	}
	switch c := s[1]; {
	case c == '[':
		// CSI: parameters, intermediates, a final byte
		for i := 2; i < len(s); i++ {
			switch b := s[i]; {
			case b >= 0x40 && b <= 0x7e:
				return i + 1
			case b < 0x20 || b > 0x7e:
				// not a CSI byte: the sequence ends before it
				return i
			}
		}
		return len(s)
	case c == ']' || c == 'P' || c == 'X' || c == '^' || c == '_':
		// OSC, DCS, SOS, PM, APC: a string up to BEL or ST (ESC \)
		for i := 2; i < len(s); i++ {
			switch {
			case s[i] == 0x07:
				return i + 1
			case s[i] == esc && i+1 < len(s) && s[i+1] == '\\':
				return i + 2
			}
		}
		return len(s)
	case c >= 0x20 && c <= 0x2f:
		// intermediates, then a final byte
		for i := 2; i < len(s); i++ {
			if b := s[i]; b >= 0x30 && b <= 0x7e {
				return i + 1
			} else if b < 0x20 || b > 0x2f {
				return i
			}
		}
		return len(s)
	case c >= 0x30 && c <= 0x7e:
		return 2
	}
	// a lone ESC
	return 1
}

// isSGR is true for a CSI ... m sequence of digits, ';' and ':' only.
func isSGR(seq string) bool {
	if len(seq) < 3 || seq[1] != '[' || seq[len(seq)-1] != 'm' {
		return false
	}
	for i := 2; i < len(seq)-1; i++ {
		if c := seq[i]; (c < '0' || c > '9') && c != ';' && c != ':' {
			return false
		}
	}
	return true
}
