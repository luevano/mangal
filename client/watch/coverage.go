package watch

import (
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/luevano/libmangal/mangadata"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// Coverage is what the provider (the numbers it lists) misses of the library's chapters
// (have), "" when it has enough: none at or below the library's last (it only has newer
// ones, or none), or under min (0-1) of the library's chapters (it lost most of them, or
// only lists the latest few). Either way the manga may need another provider. Nothing for
// a library without chapters.
func Coverage(listed []float32, have map[float32]bool, min float64) string {
	if len(have) == 0 {
		return ""
	}
	lib := slices.Sorted(maps.Keys(have))
	last := lib[len(lib)-1]
	listed = slices.Clone(listed)
	slices.Sort(listed)
	listed = slices.Compact(listed)
	library := "library " + span(lib)
	if len(listed) == 0 {
		return "the provider lists no chapters (" + library + ")"
	}
	if listed[0] > last {
		return fmt.Sprintf("the provider only has chapters after the library's (%s, provider %s)", library, span(listed))
	}
	common := 0
	for _, n := range listed {
		if have[n] {
			common++
		}
	}
	if float64(common) < min*float64(len(lib)) {
		return fmt.Sprintf("the provider has %d of the library's %d chapters (%s, provider %s)", common, len(lib), library, span(listed))
	}
	return ""
}

// span is "1-20 (20)", sorted numbers' first and last and how many.
func span(ns []float32) string {
	first, last := stringutil.FormatFloat32(ns[0]), stringutil.FormatFloat32(ns[len(ns)-1])
	if len(ns) == 1 {
		return first
	}
	return fmt.Sprintf("%s-%s (%d)", first, last, len(ns))
}

// Continuation splits the picked chapters of a watch whose provider lacks the library's
// (Coverage): the ones that continue the library's last chapter without a gap (no whole
// number missing in between) and the ones at or below it (gaps, retries) are kept, the
// newer ones after the first gap are held back, their numbers sorted.
func Continuation(picked []mangadata.Chapter, last float32) (kept []mangadata.Chapter, heldBack []float32) {
	var newer []mangadata.Chapter
	for _, ch := range picked {
		if ch.Info().Number <= last {
			kept = append(kept, ch)
		} else {
			newer = append(newer, ch)
		}
	}
	slices.SortStableFunc(newer, func(a, b mangadata.Chapter) int {
		x, y := a.Info().Number, b.Info().Number
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	})
	prev := last
	for i, ch := range newer {
		n := ch.Info().Number
		if math.Floor(float64(n)) > math.Floor(float64(prev))+1 {
			for _, rest := range newer[i:] {
				heldBack = append(heldBack, rest.Info().Number)
			}
			break
		}
		kept, prev = append(kept, ch), n
	}
	return kept, heldBack
}
