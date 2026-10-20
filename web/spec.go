package web

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
)

// Spec is the API's OpenAPI spec (openapi.yaml): the browse routes of `mangal web` and
// the API v1 of `mangal serve`.
//
//go:embed openapi.yaml
var Spec []byte

// Operation is an operation of the spec.
type Operation struct {
	Method string
	// Path is the full path ("/api/v1/status").
	Path string
	ID   string
	// Scope is the token scope it needs (x-mangal-scope), read when not said.
	Scope string
	// Plan is a plan/apply operation (x-mangal-plan): Scope is for apply, a plan is read.
	Plan bool
	// Admin needs every permission (x-mangal-admin), whatever Scope says.
	Admin   bool
	Summary string
	// V1 is an operation of the API v1, else of the browse routes.
	V1         bool
	Deprecated bool
}

var operations = sync.OnceValues(func() ([]Operation, error) {
	doc, err := openapi3.NewLoader().LoadFromData(Spec)
	if err != nil {
		return nil, fmt.Errorf("load the API spec: %w", err)
	}
	base := "/api"
	var ops []Operation
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			o := Operation{Method: method, Path: base + path, ID: op.OperationID, Scope: "read",
				Summary: op.Summary, V1: slices.Contains(op.Tags, "v1"), Deprecated: op.Deprecated}
			if o.Summary == "" {
				o.Summary = op.Description
			}
			if s, ok := extension[string](op.Extensions, "x-mangal-scope"); ok {
				o.Scope = s
			}
			o.Plan, _ = extension[bool](op.Extensions, "x-mangal-plan")
			o.Admin, _ = extension[bool](op.Extensions, "x-mangal-admin")
			ops = append(ops, o)
		}
	}
	slices.SortFunc(ops, func(a, b Operation) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		return strings.Compare(a.Method, b.Method)
	})
	return ops, nil
})

// Operations are the spec's operations, by path and method.
func Operations() ([]Operation, error) { return operations() }

// OperationByID is the operation with the id.
func OperationByID(id string) (Operation, bool) {
	ops, err := Operations()
	if err != nil {
		return Operation{}, false
	}
	for _, o := range ops {
		if strings.EqualFold(o.ID, id) {
			return o, true
		}
	}
	return Operation{}, false
}

func extension[T any](ext map[string]any, key string) (T, bool) {
	var zero T
	switch v := ext[key].(type) {
	case T:
		return v, true
	case json.RawMessage:
		var t T
		if err := json.Unmarshal(v, &t); err == nil {
			return t, true
		}
	}
	return zero, false
}

//go:generate go run ./internal/scopesgen ui/src/api/scopes.json

// UIScopesJSON is the web UI's api/scopes.json: what each v1 operation needs, by
// "METHOD /v1/path": its scope, or admin.
func UIScopesJSON() ([]byte, error) {
	ops, err := Operations()
	if err != nil {
		return nil, err
	}
	scopes := map[string]string{}
	for _, o := range ops {
		if !o.V1 {
			continue
		}
		s := o.Scope
		if o.Admin {
			s = "admin"
		}
		scopes[o.Method+" "+strings.TrimPrefix(o.Path, "/api")] = s
	}
	b, err := json.MarshalIndent(scopes, "", "  ")
	return append(b, '\n'), err
}
