package config

import (
	"strings"
	"testing"
)

const commented = `# mangal config
icons = "ascii" # or nerd

# where the cache goes
[cache]
ttl = "24h"

[download]
# the library
path = '~/manga'   # trailing
format = "PDF"
manga.cover = true

[download.metadata]
strict = true

  # indented comment
  comicinfo_xml = false

[providers]
parallelism = 15

[providers.mirrors]
# preferred sites
mango-a = "https://a.example/"
"mango.b" = "https://b.example/"

[providers.filter]
nsfw = true
`

func TestEditTOML(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		key  string
		raw  any
		// the lines of commented replaced by these (old -> new), "" old is an insertion
		// after the line in after
		want string
	}{
		{
			name: "bool in place",
			doc:  commented,
			key:  "download.metadata.strict",
			raw:  false,
			want: strings.Replace(commented, "strict = true", "strict = false", 1),
		},
		{
			name: "string keeps inline comment and quotes",
			doc:  commented,
			key:  "download.path",
			raw:  "/data/it's here",
			want: strings.Replace(commented, `path = '~/manga'   # trailing`, `path = "/data/it's here"   # trailing`, 1),
		},
		{
			name: "literal stays literal",
			doc:  commented,
			key:  "download.path",
			raw:  "/data",
			want: strings.Replace(commented, `path = '~/manga'`, `path = '/data'`, 1),
		},
		{
			name: "root key",
			doc:  commented,
			key:  "icons",
			raw:  "nerd",
			want: strings.Replace(commented, `icons = "ascii"`, `icons = "nerd"`, 1),
		},
		{
			name: "int",
			doc:  commented,
			key:  "providers.parallelism",
			raw:  int64(4),
			want: strings.Replace(commented, "parallelism = 15", "parallelism = 4", 1),
		},
		{
			name: "dotted key in place",
			doc:  commented,
			key:  "download.manga.cover",
			raw:  false,
			want: strings.Replace(commented, "manga.cover = true", "manga.cover = false", 1),
		},
		{
			name: "missing key of a dotted table",
			doc:  commented,
			key:  "download.manga.banner",
			raw:  true,
			want: strings.Replace(commented, "manga.cover = true\n", "manga.cover = true\nmanga.banner = true\n", 1),
		},
		{
			name: "missing key goes to its table with its indent",
			doc:  commented,
			key:  "download.metadata.series_json",
			raw:  false,
			want: strings.Replace(commented, "  comicinfo_xml = false\n", "  comicinfo_xml = false\n  series_json = false\n", 1),
		},
		{
			name: "missing table after its family",
			doc:  commented,
			key:  "providers.headless.use_flaresolverr",
			raw:  true,
			want: strings.Replace(commented, "nsfw = true\n", "nsfw = true\n\n[providers.headless]\nuse_flaresolverr = true\n", 1),
		},
		{
			name: "missing table at the end",
			doc:  commented,
			key:  "tui.skip_home",
			raw:  false,
			want: commented + "\n[tui]\nskip_home = false\n",
		},
		{
			name: "missing root key",
			doc:  commented,
			key:  "auth",
			raw:  true,
			want: strings.Replace(commented, "icons = \"ascii\" # or nerd\n", "icons = \"ascii\" # or nerd\nauth = true\n", 1),
		},
		{
			name: "map: change, remove, add",
			doc:  commented,
			key:  "providers.mirrors",
			raw:  map[string]any{"mango-a": "https://a2.example/", "mango-c": "https://c.example/"},
			want: strings.Replace(commented,
				"mango-a = \"https://a.example/\"\n\"mango.b\" = \"https://b.example/\"\n",
				"mango-a = \"https://a2.example/\"\nmango-c = \"https://c.example/\"\n", 1),
		},
		{
			name: "map: new table",
			doc:  "[providers]\nparallelism = 15\n\n[read]\nformat = 'PDF'\n",
			key:  "providers.mirrors",
			raw:  map[string]any{"b": "https://b/", "a": "https://a/"},
			want: "[providers]\nparallelism = 15\n\n[providers.mirrors]\na = 'https://a/'\nb = 'https://b/'\n\n[read]\nformat = 'PDF'\n",
		},
		{
			name: "map: dotted entries",
			doc:  "[providers]\nmirrors.a = 'https://a/'\n",
			key:  "providers.mirrors",
			raw:  map[string]any{"a": "https://a/", "b": "https://b/"},
			want: "[providers]\nmirrors.a = 'https://a/'\nmirrors.b = 'https://b/'\n",
		},
		{
			name: "map: remove all keeps the header",
			doc:  commented,
			key:  "providers.mirrors",
			raw:  map[string]any{},
			want: strings.Replace(commented, "mango-a = \"https://a.example/\"\n\"mango.b\" = \"https://b.example/\"\n", "", 1),
		},
		{
			name: "super-table after its sub-table",
			doc:  "[providers.filter]\nnsfw = true\n",
			key:  "providers.parallelism",
			raw:  int64(3),
			want: "[providers.filter]\nnsfw = true\n\n[providers]\nparallelism = 3\n",
		},
		{
			name: "crlf",
			doc:  "[cache]\r\nttl = '24h'\r\n",
			key:  "cache.path",
			raw:  "/c",
			want: "[cache]\r\nttl = '24h'\r\npath = '/c'\r\n",
		},
		{
			name: "no newline at the end",
			doc:  "[cache]\nttl = '24h'",
			key:  "cache.path",
			raw:  "/c",
			want: "[cache]\nttl = '24h'\npath = '/c'\n",
		},
		{
			name: "escapes",
			doc:  "[cache]\nttl = \"24h\"\n",
			key:  "cache.ttl",
			raw:  "a\"b\\c\n",
			want: "[cache]\nttl = \"a\\\"b\\\\c\\n\"\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := editTOML([]byte(tt.doc), tt.key, tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
			if err := checkTOMLEdit([]byte(tt.doc), got, tt.key, tt.raw); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestEditTOMLCantEdit(t *testing.T) {
	for name, doc := range map[string]string{
		"inline table": "[providers]\nmirrors = { a = 'https://a/' }\n",
		"array value":  "[providers]\nmirrors = ['x']\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := editTOML([]byte(doc), "providers.mirrors", map[string]any{"b": "https://b/"}); err == nil {
				t.Error("no error")
			}
		})
	}
}

func TestWriteKeyKeepsComments(t *testing.T) {
	reset(t)
	writeConfig(t, commented)
	if err := Load(Path); err != nil {
		t.Fatal(err)
	}
	if err := Download.Metadata.Strict.Write(false); err != nil {
		t.Fatal(err)
	}
	if err := SetMirror("mango-a", ""); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(commented, "strict = true", "strict = false", 1)
	want = strings.Replace(want, "mango-a = \"https://a.example/\"\n", "", 1)
	if got := readConfig(t); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if Download.Metadata.Strict.Get() {
		t.Error("not set")
	}
}

func TestWriteKeyFallsBack(t *testing.T) {
	reset(t)
	writeConfig(t, "# gone\n[providers]\nmirrors = { a = 'https://a/' }\n")
	if err := Load(Path); err != nil {
		t.Fatal(err)
	}
	if err := SetMirror("b", "https://b/"); err != nil {
		t.Fatal(err)
	}
	if got := Providers.Mirrors.Get(); got["a"] != "https://a/" || got["b"] != "https://b/" {
		t.Errorf("mirrors %v", got)
	}
	if file := readConfig(t); !strings.Contains(file, "https://b/") {
		t.Errorf("file:\n%s", file)
	}
}
