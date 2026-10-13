package watch

import (
	"fmt"
	"math"
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
		if err != nil || !validNumber(n) {
			return nil, fmt.Errorf("chapter number %q: not a number", part)
		}
		ns = append(ns, float32(n))
	}
	return ns, nil
}

// validNumber is a chapter number: not negative, NaN nor infinite.
func validNumber(n float64) bool {
	return n >= 0 && !math.IsInf(n, 0)
}

// checkFrom is an error for a From that isn't a chapter number.
func checkFrom(from *float32) error {
	if from != nil && !validNumber(float64(*from)) {
		return fmt.Errorf("from %v: not a chapter number", *from)
	}
	return nil
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
