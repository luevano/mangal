package config

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// errCantEdit is a change the in-place editor doesn't handle (e.g. a key given as an
// inline table), the file is re-serialised instead.
var errCantEdit = errors.New("can't edit in place")

// tomlKV is a key/value of the document.
type tomlKV struct {
	key []string
	// value range, end is -1 for values that aren't a scalar (arrays, inline tables)
	start, end int
	// the line(s) of the key/value, lineEnd after its newline
	lineStart, lineEnd int
	table              *tomlTable
	quote              byte
}

// tomlTable is a table of the document, the root one has no header.
type tomlTable struct {
	path   []string
	header bool
	array  bool
	// where a key is added: after its last key/value or its header, -1 when unknown
	insertAt int
	// start of the header line
	start    int
	kvs      []*tomlKV
	defaults []*tomlDefault
}

// tomlDefault is a commented default of a key ("# key = value", see Write), a comment
// line naming a registered or retired key.
type tomlDefault struct {
	key []string
	// the line, lineEnd after its newline
	lineStart, lineEnd int
	table              *tomlTable
}

type tomlDoc struct {
	data     []byte
	nl       string
	tables   []*tomlTable
	kvs      []*tomlKV
	defaults []*tomlDefault
	// quote of the first string value, new strings use it
	quote byte
}

func parseTOMLDoc(data []byte) (*tomlDoc, error) {
	d := &tomlDoc{data: data, nl: "\n"}
	if bytes.Contains(data, []byte("\r\n")) {
		d.nl = "\r\n"
	}
	root := &tomlTable{insertAt: -1}
	d.tables = append(d.tables, root)
	current := root

	p := unstable.Parser{}
	p.Reset(data)
	for p.NextExpression() {
		expr := p.Expression()
		switch expr.Kind {
		case unstable.Table, unstable.ArrayTable:
			path, first, last := keyParts(&p, expr.Key())
			current = &tomlTable{
				path:   path,
				header: true,
				array:  expr.Kind == unstable.ArrayTable,
				start:  d.lineStart(first),
			}
			current.insertAt = d.lineEnd(last)
			d.tables = append(d.tables, current)
		case unstable.KeyValue:
			rel, first, _ := keyParts(&p, expr.Key())
			kv := &tomlKV{
				key:       append(slices.Clone(current.path), rel...),
				start:     -1,
				end:       -1,
				lineStart: d.lineStart(first),
				lineEnd:   -1,
				table:     current,
			}
			value := expr.Value()
			var r unstable.Range
			switch value.Kind {
			case unstable.String, unstable.Integer, unstable.Float:
				r = value.Raw
			case unstable.Bool, unstable.DateTime, unstable.LocalDate, unstable.LocalDateTime, unstable.LocalTime:
				r = p.Range(value.Data)
			}
			if r.Length > 0 {
				kv.start = int(r.Offset)
				kv.end = int(r.Offset + r.Length)
				kv.lineEnd = d.lineEnd(kv.end)
				if value.Kind == unstable.String {
					kv.quote = data[kv.start]
					if d.quote == 0 {
						d.quote = kv.quote
					}
				}
			}
			current.insertAt = kv.lineEnd
			current.kvs = append(current.kvs, kv)
			d.kvs = append(d.kvs, kv)
		}
	}
	if err := p.Error(); err != nil {
		return nil, err
	}
	d.findDefaults()
	return d, nil
}

var commentedKey = regexp.MustCompile(`^[ \t]*#[ \t]*([A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)*)[ \t]*=`)

// findDefaults finds the commented defaults: the comment lines "# key = ..." of a
// registered (not a map) or retired key, outside the values.
func (d *tomlDoc) findDefaults() {
	for start := 0; start < len(d.data); {
		end := d.lineEnd(start)
		m := commentedKey.FindSubmatch(d.data[start:end])
		if m == nil || d.inValue(start) {
			start = end
			continue
		}
		t := d.tables[0]
		for _, table := range d.tables[1:] {
			if table.start <= start {
				t = table
			}
		}
		key := append(slices.Clone(t.path), strings.Split(string(m[1]), ".")...)
		if !t.array && isListedKey(strings.Join(key, ".")) {
			def := &tomlDefault{key: key, lineStart: start, lineEnd: end, table: t}
			t.defaults = append(t.defaults, def)
			d.defaults = append(d.defaults, def)
			if end > t.insertAt && t.insertAt >= 0 && (t.header || len(t.kvs) > 0) {
				t.insertAt = end
			}
		}
		start = end
	}
}

// isListedKey is true for the keys a commented default can be of.
func isListedKey(key string) bool {
	if f, ok := fields[key]; ok {
		return !f.isMap()
	}
	_, ok := retired[key]
	return ok
}

// inValue is true when offset is inside a value spanning lines (a multi-line string).
func (d *tomlDoc) inValue(offset int) bool {
	for _, kv := range d.kvs {
		if kv.start >= 0 && kv.start < offset && offset < kv.end {
			return true
		}
	}
	return false
}

func (d *tomlDoc) findDefault(key []string) *tomlDefault {
	for _, def := range d.defaults {
		if slices.Equal(def.key, key) {
			return def
		}
	}
	return nil
}

// keyParts returns the parts of a key and the offsets where it starts and ends.
func keyParts(p *unstable.Parser, it unstable.Iterator) (parts []string, start, end int) {
	start = -1
	for it.Next() {
		n := it.Node()
		parts = append(parts, string(n.Data))
		if start < 0 {
			start = int(n.Raw.Offset)
		}
		end = int(n.Raw.Offset + n.Raw.Length)
	}
	return parts, start, end
}

func (d *tomlDoc) lineStart(offset int) int {
	return bytes.LastIndexByte(d.data[:offset], '\n') + 1
}

// lineEnd is the offset after the newline that ends the line of offset.
func (d *tomlDoc) lineEnd(offset int) int {
	i := bytes.IndexByte(d.data[offset:], '\n')
	if i < 0 {
		return len(d.data)
	}
	return offset + i + 1
}

func (d *tomlDoc) find(key []string) *tomlKV {
	for _, kv := range d.kvs {
		if slices.Equal(kv.key, key) && !kv.table.array {
			return kv
		}
	}
	return nil
}

// edit is a replacement of data[start:end].
type edit struct {
	start, end int
	text       string
}

// editTOML returns the document with key set to raw (a string, bool, int64, or for a map
// key a map[string]any whose entries replace the table's), keeping the comments, order
// and formatting of everything else.
func editTOML(data []byte, key string, raw any) ([]byte, error) {
	d, err := parseTOMLDoc(data)
	if err != nil {
		return nil, err
	}
	path := strings.Split(key, ".")
	var edits []edit
	if m, ok := raw.(map[string]any); ok {
		if kv := d.find(path); kv != nil {
			// e.g. an inline table
			return nil, fmt.Errorf("%w: %s isn't a table", errCantEdit, key)
		}
		var added []edit
		for _, kv := range d.kvs {
			if kv.table.array || len(kv.key) != len(path)+1 || !slices.Equal(kv.key[:len(path)], path) {
				continue
			}
			v, keep := m[kv.key[len(path)]]
			if !keep {
				if kv.lineEnd < 0 {
					return nil, fmt.Errorf("%w: %s", errCantEdit, strings.Join(kv.key, "."))
				}
				edits = append(edits, edit{start: kv.lineStart, end: kv.lineEnd})
				continue
			}
			e, err := d.replace(kv, v)
			if err != nil {
				return nil, err
			}
			edits = append(edits, e)
		}
		for _, id := range slices.Sorted(maps.Keys(m)) {
			entry := append(slices.Clone(path), id)
			if d.find(entry) != nil {
				continue
			}
			e, err := d.insert(entry, m[id], added, "")
			if err != nil {
				return nil, err
			}
			added = append(added, e)
		}
		edits = append(edits, added...)
	} else if kv := d.find(path); kv != nil {
		e, err := d.replace(kv, raw)
		if err != nil {
			return nil, err
		}
		edits = append(edits, e)
	} else if def := d.findDefault(path); def != nil {
		// the key's commented default becomes its line
		e, err := d.defaultLine(def, raw, "")
		if err != nil {
			return nil, err
		}
		edits = append(edits, e)
	} else {
		e, err := d.insert(path, raw, nil, "")
		if err != nil {
			return nil, err
		}
		edits = append(edits, e)
	}
	return apply(data, edits), nil
}

// apply applies the edits, inserts at the same offset keep their order.
func apply(data []byte, edits []edit) []byte {
	slices.SortStableFunc(edits, func(a, b edit) int { return a.start - b.start })
	var out bytes.Buffer
	last := 0
	for _, e := range edits {
		out.Write(data[last:e.start])
		out.WriteString(e.text)
		last = e.end
	}
	out.Write(data[last:])
	return out.Bytes()
}

func (d *tomlDoc) replace(kv *tomlKV, raw any) (edit, error) {
	if kv.end < 0 {
		return edit{}, fmt.Errorf("%w: %s isn't a scalar", errCantEdit, strings.Join(kv.key, "."))
	}
	quote := kv.quote
	if quote == 0 {
		quote = d.quote
	}
	text, err := encodeTOMLValue(raw, quote)
	if err != nil {
		return edit{}, err
	}
	return edit{start: kv.start, end: kv.end, text: text}, nil
}

// defaultTOML returns the document with key as a commented default ("# key = value"): its
// line (or its commented default) replaced, or a commented default added to its table.
func defaultTOML(data []byte, key string, raw any) ([]byte, error) {
	d, err := parseTOMLDoc(data)
	if err != nil {
		return nil, err
	}
	path := strings.Split(key, ".")
	var edits []edit
	def := d.findDefault(path)
	if kv := d.find(path); kv != nil {
		if kv.lineEnd < 0 || kv.lineStart != d.lineStart(kv.start) {
			// a value spanning lines, or not on a line of its own
			return nil, fmt.Errorf("%w: %s", errCantEdit, key)
		}
		text, err := d.keyLine(kv.table, path, raw, indentOf(d.data[kv.lineStart:]), "# ")
		if err != nil {
			return nil, err
		}
		// its trailing comment stays
		text = strings.TrimSuffix(text, d.nl) + string(d.data[kv.end:kv.lineEnd])
		if d.isDefaultValue(kv, strings.Join(path, ".")) {
			// as written (e.g. an enum's case)
			line := d.data[kv.lineStart:kv.lineEnd]
			indent := indentOf(line)
			text = indent + "# " + string(line[len(indent):])
		}
		edits = append(edits, edit{start: kv.lineStart, end: kv.lineEnd, text: text})
		if def != nil {
			edits = append(edits, edit{start: def.lineStart, end: def.lineEnd})
		}
	} else if def != nil {
		e, err := d.defaultLine(def, raw, "# ")
		if err != nil {
			return nil, err
		}
		if string(d.data[e.start:e.end]) == e.text {
			return data, nil
		}
		edits = append(edits, e)
	} else {
		e, err := d.insert(path, raw, nil, "# ")
		if err != nil {
			return nil, err
		}
		edits = append(edits, e)
	}
	return apply(data, edits), nil
}

// isDefaultValue is true when the key/value has the key's default.
func (d *tomlDoc) isDefaultValue(kv *tomlKV, key string) bool {
	f, ok := fields[key]
	if !ok {
		return false
	}
	var v struct{ V any }
	if err := toml.Unmarshal(append([]byte("V = "), d.data[kv.start:kv.end]...), &v); err != nil {
		return false
	}
	raw, err := f.toRaw(v.V)
	return err == nil && isDefault(key, raw)
}

// removeTOMLDefault returns the document without the key's commented default.
func removeTOMLDefault(data []byte, key string) ([]byte, error) {
	d, err := parseTOMLDoc(data)
	if err != nil {
		return nil, err
	}
	def := d.findDefault(strings.Split(key, "."))
	if def == nil {
		return data, nil
	}
	t := def.table
	return apply(data, d.removeLine(def.lineStart, def.lineEnd, t, len(t.kvs) == 0 && len(t.defaults) == 1)), nil
}

// removeLine removes data[start:end] and, when emptied, the header of table t with the
// blank line after it when there's one before it too.
func (d *tomlDoc) removeLine(start, end int, t *tomlTable, emptied bool) []edit {
	edits := []edit{{start: start, end: end}}
	if !emptied || !t.header || t.array {
		return edits
	}
	header := edit{start: t.start, end: d.lineEnd(t.start)}
	last := max(end, header.end)
	blankBefore := t.start == 0 || (t.start >= 2 && d.data[t.start-1] == '\n' && len(bytes.TrimSpace(d.data[d.lineStart(t.start-1):t.start])) == 0)
	if blankBefore && last < len(d.data) {
		if next := d.lineEnd(last); len(bytes.TrimSpace(d.data[last:next])) == 0 {
			edits = append(edits, edit{start: last, end: next})
		}
	}
	if header.end <= start || header.start >= end {
		edits = append(edits, header)
	}
	slices.SortFunc(edits, func(a, b edit) int { return a.start - b.start })
	return edits
}

// defaultLine replaces the commented default's line with the key set to raw, commented
// with prefix "# ".
func (d *tomlDoc) defaultLine(def *tomlDefault, raw any, prefix string) (edit, error) {
	text, err := d.keyLine(def.table, def.key, raw, indentOf(d.data[def.lineStart:]), prefix)
	if err != nil {
		return edit{}, err
	}
	return edit{start: def.lineStart, end: def.lineEnd, text: text}, nil
}

// keyLine is the line of the key in table t.
func (d *tomlDoc) keyLine(t *tomlTable, key []string, raw any, indent, prefix string) (string, error) {
	value, err := encodeTOMLValue(raw, d.quote)
	if err != nil {
		return "", err
	}
	return indent + prefix + encodeTOMLKey(key[len(t.path):]) + " = " + value + d.nl, nil
}

// insert adds the key to the table that has it (the closest one defining its parent),
// or to a new table after its relatives; previous are the inserts already made, a new
// table made by one of them takes the next ones. prefix "# " adds it commented.
func (d *tomlDoc) insert(key []string, raw any, previous []edit, prefix string) (edit, error) {
	value, err := encodeTOMLValue(raw, d.quote)
	if err != nil {
		return edit{}, err
	}
	parent := key[:len(key)-1]
	// a parent that's a value (e.g. an inline table) can't take keys
	for i := 1; i <= len(parent); i++ {
		if d.find(key[:i]) != nil {
			return edit{}, fmt.Errorf("%w: %s isn't a table", errCantEdit, strings.Join(key[:i], "."))
		}
	}

	if t := d.table(parent); t != nil {
		return d.insertInto(t, key, value, prefix)
	}
	// defined by dotted keys in an ancestor table
	for _, kv := range d.kvs {
		t := kv.table
		if !t.array && len(t.path) < len(parent) && slices.Equal(t.path, parent[:len(t.path)]) &&
			len(kv.key) > len(parent) && slices.Equal(kv.key[:len(parent)], parent) {
			return d.insertInto(kv.table, key, value, prefix)
		}
	}

	for _, e := range previous {
		if strings.Contains(e.text, "["+encodeTOMLKey(parent)+"]") {
			// right after the key the new table got
			line := prefix + encodeTOMLKey(key[len(parent):]) + " = " + value + d.nl
			return edit{start: e.start, end: e.end, text: line}, nil
		}
	}

	block := "[" + encodeTOMLKey(parent) + "]" + d.nl + prefix + encodeTOMLKey(key[len(parent):]) + " = " + value + d.nl
	at := -1
	// after the last table of the same family (sharing the first part)
	for _, t := range d.tables[1:] {
		if !t.array && t.path[0] == parent[0] {
			at = t.insertAt
		}
	}
	if at < 0 {
		at = len(d.data)
	}
	lead := d.nl
	if at > 0 && d.data[at-1] != '\n' {
		lead = d.nl + d.nl
	}
	if at == 0 {
		lead = ""
	}
	return edit{start: at, end: at, text: lead + block}, nil
}

// table is the table with the header (or the root), nil if there's none.
func (d *tomlDoc) table(path []string) *tomlTable {
	if len(path) == 0 {
		return d.tables[0]
	}
	for _, t := range d.tables[1:] {
		if !t.array && slices.Equal(t.path, path) {
			return t
		}
	}
	return nil
}

func (d *tomlDoc) insertInto(t *tomlTable, key []string, value, prefix string) (edit, error) {
	line := prefix + encodeTOMLKey(key[len(t.path):]) + " = " + value + d.nl
	if len(t.kvs) > 0 {
		last := t.kvs[len(t.kvs)-1]
		line = indentOf(d.data[last.lineStart:]) + line
	}
	at := t.insertAt
	if !t.header && len(t.kvs) == 0 && len(t.defaults) > 0 {
		at = t.defaults[len(t.defaults)-1].lineEnd
	} else if !t.header && len(t.kvs) == 0 {
		// before the first table and the comments over it
		at = len(d.data)
		if len(d.tables) > 1 {
			at = d.commentsOver(d.tables[1].start)
			line += d.nl
		}
	}
	if at < 0 {
		return edit{}, fmt.Errorf("%w: end of table %q", errCantEdit, strings.Join(t.path, "."))
	}
	if at > 0 && d.data[at-1] != '\n' {
		line = d.nl + line
	}
	return edit{start: at, end: at, text: line}, nil
}

// commentsOver is the start of the comment lines right over the line at offset.
func (d *tomlDoc) commentsOver(offset int) int {
	for offset > 0 {
		prev := d.lineStart(offset - 1)
		if !bytes.HasPrefix(bytes.TrimSpace(d.data[prev:offset]), []byte("#")) {
			break
		}
		offset = prev
	}
	return offset
}

func indentOf(line []byte) string {
	return string(line[:len(line)-len(bytes.TrimLeft(line, " \t"))])
}

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func encodeTOMLKey(parts []string) string {
	out := make([]string, len(parts))
	for i, p := range parts {
		if bareKey.MatchString(p) {
			out[i] = p
		} else {
			out[i] = basicString(p)
		}
	}
	return strings.Join(out, ".")
}

// encodeTOMLValue encodes a raw config value, a string in quote's style when it can.
func encodeTOMLValue(raw any, quote byte) (string, error) {
	switch v := raw.(type) {
	case string:
		if quote != '"' && canBeLiteral(v) {
			return "'" + v + "'", nil
		}
		return basicString(v), nil
	case bool:
		return strconv.FormatBool(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case int:
		return strconv.Itoa(v), nil
	default:
		return "", fmt.Errorf("%w: value of type %T", errCantEdit, raw)
	}
}

func canBeLiteral(s string) bool {
	for _, r := range s {
		if r == '\'' || r == 0x7f || (r < 0x20 && r != '\t') {
			return false
		}
	}
	return true
}

func basicString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// checkTOMLEdit parses both documents and checks that edited has the values of original
// with only key set to raw.
func checkTOMLEdit(original, edited []byte, key string, raw any) error {
	want := map[string]any{}
	if err := toml.Unmarshal(original, &want); err != nil {
		return fmt.Errorf("parse the file: %w", err)
	}
	setNested(want, strings.Split(key, "."), raw)
	got := map[string]any{}
	if err := toml.Unmarshal(edited, &got); err != nil {
		return fmt.Errorf("parse the edited file: %w", err)
	}
	normalizeTOML(want)
	normalizeTOML(got)
	if !reflect.DeepEqual(want, got) {
		return fmt.Errorf("the edited file has other values")
	}
	return nil
}

// normalizeTOML drops the empty tables (a removed map is no table or an empty one) and
// makes the ints int64.
func normalizeTOML(m map[string]any) {
	for k, v := range m {
		switch v := v.(type) {
		case map[string]any:
			normalizeTOML(v)
			if len(v) == 0 {
				delete(m, k)
			}
		case int:
			m[k] = int64(v)
		}
	}
}

// checkTOMLRemoved parses both documents and checks that edited has the values of
// original without key.
func checkTOMLRemoved(original, edited []byte, key string) error {
	want := map[string]any{}
	if err := toml.Unmarshal(original, &want); err != nil {
		return fmt.Errorf("parse the file: %w", err)
	}
	deleteNested(want, strings.Split(key, "."))
	got := map[string]any{}
	if err := toml.Unmarshal(edited, &got); err != nil {
		return fmt.Errorf("parse the edited file: %w", err)
	}
	normalizeTOML(want)
	normalizeTOML(got)
	if !reflect.DeepEqual(want, got) {
		return fmt.Errorf("the edited file has other values")
	}
	return nil
}

// removeTOMLKey returns the document without the key's line, and without its table's
// header when the key was all the table had.
func removeTOMLKey(data []byte, key string) ([]byte, error) {
	d, err := parseTOMLDoc(data)
	if err != nil {
		return nil, err
	}
	kv := d.find(strings.Split(key, "."))
	if kv == nil {
		return data, nil
	}
	if kv.lineEnd < 0 {
		return nil, fmt.Errorf("%w: %s", errCantEdit, key)
	}
	t := kv.table
	return apply(data, d.removeLine(kv.lineStart, kv.lineEnd, t, len(t.kvs) == 1 && len(t.defaults) == 0)), nil
}
