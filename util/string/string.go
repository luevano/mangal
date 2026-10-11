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
