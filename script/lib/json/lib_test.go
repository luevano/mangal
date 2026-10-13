package json

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

// print runs the script with the json lib, what json.print printed is out.
func print(t *testing.T, script string) (out string, err error) {
	t.Helper()
	r, w, perr := os.Pipe()
	if perr != nil {
		t.Fatal(perr)
	}
	stdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = stdout }()

	state := lua.NewState()
	defer state.Close()
	// as script.Run
	state.SetContext(context.Background())
	state.PreloadModule(libName, Lib().Loader())
	err = state.DoString(`local json = require("json")` + "\n" + script)
	w.Close()
	data, _ := io.ReadAll(r)
	return string(data), err
}

func TestPrint(t *testing.T) {
	tests := []struct{ script, want string }{
		{`json.print({a = 1, b = {"x", "y"}})`, `{"a":1,"b":["x","y"]}` + "\n"},
		{`json.print({1, 2, 3}, ".[] | select(. > 1)")`, "2\n3\n"},
		{`json.print({name = "M", chapters = {{n = 1}, {n = 2}}}, ".chapters[].n")`, "1\n2\n"},
	}
	for _, tt := range tests {
		out, err := print(t, tt.script)
		if err != nil || out != tt.want {
			t.Errorf("%s: %q, %v; want %q", tt.script, out, err, tt.want)
		}
	}
}

func TestPrintErrors(t *testing.T) {
	for script, want := range map[string]string{
		`local t = {}; t.self = t; json.print(t)`: "cycl",
		`json.print({1}, ".[")`:                   "",
		`json.print({a = 1}, "error(\"boom\")")`:  "boom",
	} {
		if _, err := print(t, script); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", script, err)
		}
	}
}
