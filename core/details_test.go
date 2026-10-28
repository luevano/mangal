package core

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/luevano/mangal/v5/library"
)

// fill sets every exported field of v (a pointer) to something not zero.
func fill(t *testing.T, v reflect.Value, depth int) {
	t.Helper()
	if depth > 8 {
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fill(t, v.Elem(), depth+1)
	case reflect.Struct:
		if v.Type() == reflect.TypeFor[time.Time]() {
			v.Set(reflect.ValueOf(time.Unix(1700000000+int64(depth), 0).UTC()))
			return
		}
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if !f.IsExported() || f.Tag.Get("json") == "-" {
				continue
			}
			fill(t, v.Field(i), depth+1)
		}
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fill(t, v.Index(0), depth+1)
	case reflect.Array:
		for i := range v.Len() {
			fill(t, v.Index(i), depth+1)
		}
	case reflect.Map:
		v.Set(reflect.MakeMap(v.Type()))
		k, e := reflect.New(v.Type().Key()).Elem(), reflect.New(v.Type().Elem()).Elem()
		fill(t, k, depth+1)
		fill(t, e, depth+1)
		v.SetMapIndex(k, e)
	case reflect.String:
		v.SetString("x")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(depth + 1))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(uint64(depth + 1))
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(depth) + 0.5)
	case reflect.Interface:
		if v.Type() == reflect.TypeFor[error]() {
			v.Set(reflect.ValueOf(errors.New("an error")))
			return
		}
		t.Errorf("%s: an interface the API can't decode", v.Type())
	default:
		t.Errorf("%s: unsupported kind %s", v.Type(), v.Kind())
	}
}

// TestDetailsRoundTrip: every kind's plan and result details come back from their JSON as
// the same Go values.
func TestDetailsRoundTrip(t *testing.T) {
	for _, kind := range Kinds {
		types, ok := detailTypes[kind]
		if !ok {
			t.Errorf("%s: no detail types", kind)
			continue
		}
		for which, typ := range types {
			if typ == nil {
				continue
			}
			v := reflect.New(typ)
			fill(t, v.Elem(), 0)
			detail := v.Elem().Interface()
			var got any
			if which == 0 {
				b, err := json.Marshal(Plan{Kind: kind, Lines: []string{}, Detail: detail})
				if err != nil {
					t.Fatalf("%s plan: %s", kind, err)
				}
				var p Plan
				if err := json.Unmarshal(b, &p); err != nil {
					t.Fatalf("%s plan: %s", kind, err)
				}
				got = p.Detail
			} else {
				b, err := json.Marshal(OpResult{Kind: kind, Detail: detail})
				if err != nil {
					t.Fatalf("%s result: %s", kind, err)
				}
				var r OpResult
				if err := json.Unmarshal(b, &r); err != nil {
					t.Fatalf("%s result: %s", kind, err)
				}
				got = r.Detail
			}
			// errors come back as their messages
			if up, ok := got.(library.UndoPlan); ok {
				want := detail.(library.UndoPlan)
				if up.ChangedErr.Error() != want.ChangedErr.Error() || up.SnapshotErr.Error() != want.SnapshotErr.Error() {
					t.Errorf("%s: the errors changed", kind)
				}
				up.ChangedErr, up.SnapshotErr = want.ChangedErr, want.SnapshotErr
				got = up
			}
			if ud, ok := got.(UndoDone); ok {
				want := detail.(UndoDone)
				ud.Plan.ChangedErr, ud.Plan.SnapshotErr = want.Plan.ChangedErr, want.Plan.SnapshotErr
				got = ud
			}
			if !reflect.DeepEqual(got, detail) {
				gb, _ := json.Marshal(got)
				wb, _ := json.Marshal(detail)
				t.Errorf("%s detail %d: got\n%s\nwant\n%s", kind, which, gb, wb)
			}
		}
	}
	for kind := range detailTypes {
		if !slices.Contains(Kinds, kind) {
			t.Errorf("%s: detail types of no kind", kind)
		}
	}
}
