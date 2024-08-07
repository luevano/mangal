package string

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/fatih/camelcase"
	"github.com/luevano/mangal/theme/style"
	"golang.org/x/exp/constraints"
)

// Trim trims a string to a maximum length, appending an ellipsis if necessary.
// The trimmed string with ellipsis will never be longer than max.
// Works with ANSI escape codes.
func Trim(s string, max int) string {
	if max <= 0 {
		panic("max must be greater than 0")
	}

	stringLength := lipgloss.Width(s)

	if s == "" {
		return s
	}

	const ellipsis = '…'

	if max == 1 {
		return string(ellipsis)
	}

	if stringLength < max {
		return s
	}

	trimmed := style.Trim(max - 1).Render(s)

	// get index of \x1b
	idx := strings.LastIndex(trimmed, "\x1b")
	if idx == -1 {
		return trimmed + string(ellipsis)
	}

	// insert ellipsis before \x1b
	return trimmed[:idx] + string(ellipsis) + trimmed[idx:]
}

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

// FormatRanges formats a slice of integers into a string of ranges.
//
//	FormatRanges([]int{1, 2, 3}) // "1-3"
//	FormatRanges([]int{1, 2, 4}) // "1-2, 4"
//	FormatRanges([]int{1, 3, 5}) // "1, 3, 5"
func FormatRanges[T constraints.Integer | constraints.Float](ranges []T) string {
	if len(ranges) == 0 {
		return ""
	}

	var (
		rangesStr []string
		start     = ranges[0]
		prev      = ranges[0]
	)

	for _, r := range ranges[1:] {
		if r-prev == 1 {
			prev = r
			continue
		}

		if start == prev {
			rangesStr = append(rangesStr, fmt.Sprint(start))
		} else {
			rangesStr = append(rangesStr, fmt.Sprint(start, "-", prev))
		}

		start = r
		prev = r
	}

	if start == prev {
		rangesStr = append(rangesStr, fmt.Sprint(start))
	} else {
		rangesStr = append(rangesStr, fmt.Sprint(start, "-", prev))
	}

	return strings.Join(rangesStr, ", ")
}

// FormatFloa32 returns the string representation with
// all of the insignificant digits removed.
//
// For example, "001.500" becomes "1.5".
func FormatFloa32(n float32) string {
	return strconv.FormatFloat(float64(n), 'f', -1, 64)
}

// FormatFloa64 returns the string representation with
// all of the insignificant digits removed.
//
// For example, "001.500" becomes "1.5".
func FormatFloa64(n float64) string {
	return strconv.FormatFloat(n, 'f', -1, 64)
}

func CaseCamelToSnake(s string) string {
	words := camelcase.Split(s)
	for i, word := range words {
		words[i] = strings.ToLower(word)
	}

	return strings.Join(words, "_")
}
