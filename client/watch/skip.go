package watch

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	stringutil "github.com/luevano/mangal/v5/util/string"
)

// ParseNumbers parses chapter numbers separated by commas or spaces ("189.5, 190").
func ParseNumbers(s string) ([]float32, error) {
	var ns []float32
	for part := range strings.FieldsFuncSeq(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		n, err := strconv.ParseFloat(part, 32)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("chapter number %q: not a number", part)
		}
		ns = append(ns, float32(n))
	}
	return ns, nil
}

// FormatNumbers is the chapter numbers separated by commas: "189.5, 190".
func FormatNumbers(ns []float32) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = stringutil.FormatFloa32(n)
	}
	return strings.Join(parts, ", ")
}

// AddNumbers is list with add, sorted, each number once.
func AddNumbers(list, add []float32) []float32 {
	ns := slices.Concat(list, add)
	slices.Sort(ns)
	return slices.Compact(ns)
}

// RemoveNumbers is list without the numbers in remove.
func RemoveNumbers(list, remove []float32) []float32 {
	return slices.DeleteFunc(slices.Clone(list), func(n float32) bool { return slices.Contains(remove, n) })
}
