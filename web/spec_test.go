package web

import (
	"bytes"
	"os"
	"testing"
)

// every operation of the API v1 says its scope, the plan/apply ones aren't GETs
func TestSpecOperations(t *testing.T) {
	ops, err := Operations()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	var v1 int
	for _, o := range ops {
		if ids[o.ID] {
			t.Errorf("%s twice", o.ID)
		}
		ids[o.ID] = true
		if !o.V1 {
			continue
		}
		v1++
		switch o.Scope {
		case "read", "progress", "browse", "download", "manage", "tokens", "admin", "public":
		default:
			t.Errorf("%s: scope %q", o.ID, o.Scope)
		}
		// a GET changes nothing: it needs at most the permission to see what it shows
		if o.Method == "GET" && (o.Scope == "progress" || o.Scope == "download") {
			t.Errorf("%s: a GET with scope %s", o.ID, o.Scope)
		}
		if o.Plan && o.Method == "GET" {
			t.Errorf("%s: a GET plan", o.ID)
		}
	}
	if v1 == 0 {
		t.Fatal("no v1 operations")
	}
	if o, ok := OperationByID("mergeLibraryMangas"); !ok || !o.Plan || o.Scope != "manage" || o.Path != "/api/v1/library/merge" {
		t.Errorf("merge: %+v", o)
	}
}

// the web UI's api/scopes.json is up to date (go generate ./web writes it)
func TestUIScopesJSON(t *testing.T) {
	want, err := UIScopesJSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("ui/src/api/scopes.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("web/ui/src/api/scopes.json is stale: go generate ./web")
	}
}
