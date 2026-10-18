package string

import (
	"strconv"

	"github.com/dustin/go-humanize"
	"github.com/dustin/go-humanize/english"
)

// Quantify returns a string with the quantity and the correct form of the
// word, depending on the quantity.
func Quantify(n int, singular, plural string) string {
	return english.Plural(n, singular, plural)
}

// FormatFloat32 returns the string representation with
// all of the insignificant digits removed.
//
// For example, "001.500" becomes "1.5".
func FormatFloat32(n float32) string {
	return strconv.FormatFloat(float64(n), 'f', -1, 32)
}

// HumanSize is a byte size like 12 MB or 2.5 GB (SI units).
func HumanSize(n int64) string {
	return humanize.Bytes(uint64(max(n, 0)))
}
