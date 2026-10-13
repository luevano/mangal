package string

import (
	"fmt"
	"strconv"
)

// Quantify returns a string with the quantity and the correct form of the
// word, depending on the quantity.
func Quantify(n int, singular, plural string) string {
	var form string
	if n == 1 {
		form = singular
	} else {
		form = plural
	}

	return fmt.Sprint(n, " ", form)
}

// FormatFloa32 returns the string representation with
// all of the insignificant digits removed.
//
// For example, "001.500" becomes "1.5".
func FormatFloa32(n float32) string {
	return strconv.FormatFloat(float64(n), 'f', -1, 32)
}

// HumanSize is a byte size like 12.3 MB.
func HumanSize(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGTPE"[exp])
}
