package logs

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/log"
	"github.com/sahilm/fuzzy"
)

// regexpPrefix makes a query a regexp instead of a fuzzy one.
const regexpPrefix = "re:"

// matcher finds a query in the entries' messages (not the time or the level: the level is
// tab's, and fuzzy over "DEBUG " would make short queries match most lines). The query is
// fuzzy as fzf's: each word a subsequence, every word in any order; "re:" makes it a
// regexp. Both are case-insensitive unless the query has a capital, as mangal logs -g.
type matcher struct {
	re *regexp.Regexp
	// words of a fuzzy query, as runes
	words         [][]rune
	caseSensitive bool
}

// newMatcher is the query's matcher, nil for an empty one.
func newMatcher(query string) *matcher {
	if expr, ok := strings.CutPrefix(query, regexpPrefix); ok {
		re := log.Pattern(strings.TrimSpace(expr), false)
		if re == nil {
			return nil
		}
		return &matcher{re: re}
	}
	m := &matcher{caseSensitive: strings.ToLower(query) != query}
	for w := range strings.FieldsSeq(query) {
		m.words = append(m.words, []rune(w))
	}
	if len(m.words) == 0 {
		return nil
	}
	return m
}

func (m *matcher) match(s string) bool {
	if m.re != nil {
		return m.re.MatchString(s)
	}
	for _, w := range m.words {
		if m.positions(w, s) == nil {
			return false
		}
	}
	return true
}

// positions are the byte indexes of s that match the word, nil for none. sahilm/fuzzy
// (the lists' filter) picks the best ones but folds case: when it picked another case, the
// first ones of the right case.
func (m *matcher) positions(w []rune, s string) []int {
	found := fuzzy.FindNoSort(string(w), []string{s})
	if len(found) == 0 {
		return nil
	}
	pos := found[0].MatchedIndexes
	if !m.caseSensitive || sameRunes(w, s, pos) {
		return pos
	}
	pos = pos[:0]
	for j, r := range s {
		if r == w[len(pos)] {
			if pos = append(pos, j); len(pos) == len(w) {
				return pos
			}
		}
	}
	return nil
}

func sameRunes(w []rune, s string, pos []int) bool {
	for i, p := range pos {
		if r, _ := utf8.DecodeRuneInString(s[p:]); r != w[i] {
			return false
		}
	}
	return true
}

// spans are the byte ranges of s that match, in order, not overlapping.
func (m *matcher) spans(s string) [][2]int {
	if m.re != nil {
		var spans [][2]int
		for _, l := range m.re.FindAllStringIndex(s, -1) {
			if l[1] > l[0] {
				spans = append(spans, [2]int{l[0], l[1]})
			}
		}
		return spans
	}
	var pos []int
	for _, w := range m.words {
		pos = append(pos, m.positions(w, s)...)
	}
	slices.Sort(pos)
	var spans [][2]int
	for _, p := range slices.Compact(pos) {
		_, size := utf8.DecodeRuneInString(s[p:])
		if n := len(spans); n > 0 && spans[n-1][1] == p {
			spans[n-1][1] = p + size
			continue
		}
		spans = append(spans, [2]int{p, p + size})
	}
	return spans
}

var (
	matchStyle = lipgloss.NewStyle().Reverse(true).Foreground(lipgloss.Color("#EBCA89"))
	// the current match's
	currentMatchStyle = lipgloss.NewStyle().Reverse(true).Bold(true).Foreground(lipgloss.Color("#E5895E"))
)

// highlight styles the spans of s, line by line (a style over a newline pads the lines).
func highlight(s string, spans [][2]int, st lipgloss.Style) string {
	if len(spans) == 0 {
		return s
	}
	var sb strings.Builder
	last := 0
	for _, sp := range spans {
		sb.WriteString(s[last:sp[0]])
		for i, line := range strings.Split(s[sp[0]:sp[1]], "\n") {
			if i > 0 {
				sb.WriteByte('\n')
			}
			if line != "" {
				sb.WriteString(st.Render(line))
			}
		}
		last = sp[1]
	}
	sb.WriteString(s[last:])
	return sb.String()
}
