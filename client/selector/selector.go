// Package selector picks mangas and chapters from a list with a selector string, as
// "mangal inline" does with its -m and -c flags.
package selector

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/luevano/libmangal/mangadata"
)

// Error is a selector that's invalid or selects nothing.
type Error struct {
	// What is "manga" or "chapter".
	What     string
	Selector string
	Reason   string
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("invalid %s selector %q", e.What, e.Selector)
	if e.Reason == "" {
		return msg
	}
	return fmt.Sprintf("%s (%s)", msg, e.Reason)
}

// ErrNoChapters is selecting from no chapters.
var ErrNoChapters = errors.New("no chapters")

var (
	idQueryRegex  = regexp.MustCompile(`(?i)^\s*m((anga)?[-_]?)?id\s*:\s*(.*\S)\s*$`)
	chapterRegex  = regexp.MustCompile(`^(?P<from>\d+(\.\d+)?)?-(?P<to>\d+(\.\d+)?)?$`)
	chapterFromID = chapterRegex.SubexpIndex("from")
	chapterToID   = chapterRegex.SubexpIndex("to")
)

// IDQuery returns the id of a query like "mid: <id>" or "manga-id: <id>", the deprecated
// way to give a manga id where a title query goes.
func IDQuery(query string) (string, bool) {
	m := idQueryRegex.FindStringSubmatch(query)
	if m == nil {
		return "", false
	}
	return m[3], true
}

// Mangas returns the indexes of the mangas the selector picks, from a search of query:
// all, first, last, id (a single result of an id query, see IDQuery), exact (title),
// closest (title) or an index. An id query defaults "all" to "id".
func Mangas(sel, query string, mangas []mangadata.Manga) ([]int, error) {
	sel = strings.ReplaceAll(sel, " ", "")
	fail := func(format string, a ...any) ([]int, error) {
		return nil, &Error{What: "manga", Selector: sel, Reason: fmt.Sprintf(format, a...)}
	}

	if _, byID := IDQuery(query); byID && sel != "id" {
		if sel != "all" {
			return fail("query %q is an id query, the selector must be id or all", query)
		}
		sel = "id"
	}
	if len(mangas) == 0 {
		return fail("no mangas")
	}

	switch sel {
	case "all":
		indexes := make([]int, len(mangas))
		for i := range mangas {
			indexes[i] = i
		}
		return indexes, nil
	case "first":
		return []int{0}, nil
	case "last":
		return []int{len(mangas) - 1}, nil
	case "id":
		if len(mangas) > 1 {
			return fail("%d mangas found by id, expected 1", len(mangas))
		}
		return []int{0}, nil
	case "exact":
		for i, m := range mangas {
			if strings.EqualFold(m.Info().Title, query) {
				return []int{i}, nil
			}
		}
		return fail("no manga titled %q", query)
	case "closest":
		index, rank := -1, math.MaxInt
		for i, m := range mangas {
			// the first of the best rank wins
			if r := fuzzy.RankMatchNormalizedFold(query, m.Info().Title); r != -1 && r < rank {
				index, rank = i, r
			}
		}
		if index == -1 {
			return fail("no manga title close to %q", query)
		}
		return []int{index}, nil
	default:
		index, err := strconv.Atoi(sel)
		if err != nil {
			return fail("not a selector or an index")
		}
		if index < 0 || index >= len(mangas) {
			return fail("index out of range (0, %d)", len(mangas)-1)
		}
		return []int{index}, nil
	}
}

// Chapters returns the chapters the selector picks: all, first, last, a chapter number
// or a range of numbers ([from]-[to], both included). The chapters are in ascending order.
func Chapters(sel string, chapters []mangadata.Chapter) ([]mangadata.Chapter, error) {
	sel = strings.ReplaceAll(sel, " ", "")
	fail := func(format string, a ...any) ([]mangadata.Chapter, error) {
		return nil, &Error{What: "chapter", Selector: sel, Reason: fmt.Sprintf(format, a...)}
	}
	if len(chapters) == 0 {
		return nil, ErrNoChapters
	}

	first, last := chapters[0].Info().Number, chapters[len(chapters)-1].Info().Number
	between := func(from, to float32) []mangadata.Chapter {
		var selected []mangadata.Chapter
		for _, ch := range chapters {
			if n := ch.Info().Number; n >= from && n <= to {
				selected = append(selected, ch)
			}
		}
		return selected
	}

	switch sel {
	case "all":
		return chapters, nil
	case "first":
		return chapters[:1], nil
	case "last":
		return chapters[len(chapters)-1:], nil
	}

	if !strings.Contains(sel, "-") {
		number, err := parseNumber(sel)
		if err != nil {
			return fail("not a selector or a chapter number")
		}
		if number < first || number > last {
			return fail("chapter number %s out of range (%s, %s)", fmtFloat(number), fmtFloat(first), fmtFloat(last))
		}
		// more than one chapter can have the number
		return between(number, number), nil
	}

	m := chapterRegex.FindStringSubmatch(sel)
	if m == nil {
		return fail("not a range")
	}
	fromS, toS := m[chapterFromID], m[chapterToID]
	if fromS == "" && toS == "" {
		return fail("no 'from' or 'to'")
	}
	from, to := first, last
	if fromS != "" {
		from, _ = parseNumber(fromS)
	}
	if toS != "" {
		to, _ = parseNumber(toS)
	}
	switch {
	case from > last:
		return fail("'from' (%s) greater than the last chapter number (%s)", fmtFloat(from), fmtFloat(last))
	case to > last:
		return fail("'to' (%s) greater than the last chapter number (%s)", fmtFloat(to), fmtFloat(last))
	case from > to:
		return fail("'from' (%s) greater than 'to' (%s)", fmtFloat(from), fmtFloat(to))
	}
	return between(from, to), nil
}

func parseNumber(s string) (float32, error) {
	n, err := strconv.ParseFloat(s, 32)
	return float32(n), err
}

func fmtFloat(n float32) string {
	return strconv.FormatFloat(float64(n), 'f', -1, 32)
}
