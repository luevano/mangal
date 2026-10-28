package core

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/health"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
)

// detailTypes are the Go types of each kind's Plan.Detail and OpResult.Detail (nil: none,
// or one that can't be decoded), so a plan or result read from the API holds what the UIs
// type-assert. The JSON is the types' own (Go field names where they have no tags).
var detailTypes = map[Kind][2]reflect.Type{
	KindMerge:        {reflect.TypeFor[library.MergePlan]()},
	KindMove:         {reflect.TypeFor[library.MovePlan]()},
	KindLink:         {reflect.TypeFor[LinkDetail]()},
	KindBackups:      {reflect.TypeFor[BackupsDetail](), reflect.TypeFor[int]()},
	KindMark:         {reflect.TypeFor[MarkDetail](), reflect.TypeFor[library.MarkResult]()},
	KindRename:       {reflect.TypeFor[client.RenamePlan](), reflect.TypeFor[client.RenameResult]()},
	KindForget:       {reflect.TypeFor[[]storage.Manga]()},
	KindDelete:       {reflect.TypeFor[DeletePlan]()},
	KindEmptyTrash:   {reflect.TypeFor[EmptyTrashPlan]()},
	KindMigrate:      {reflect.TypeFor[library.Migration]()},
	KindHistoryClear: {reflect.TypeFor[[]storage.Read](), reflect.TypeFor[int64]()},
	KindUndo:         {reflect.TypeFor[library.UndoPlan](), reflect.TypeFor[UndoDone]()},
	// a bind's is the source's metadata.Metadata: not decoded
	KindBind:        {},
	KindUnbind:      {},
	KindHealthFix:   {reflect.TypeFor[health.FixResult](), reflect.TypeFor[health.FixResult]()},
	KindDismissPick: {reflect.TypeFor[storage.LinkPick]()},
	KindRetitle:     {reflect.TypeFor[RetitleDetail](), reflect.TypeFor[int]()},
}

// decodeDetail is the kind's detail (plan 0, result 1) in raw, nil when there's none.
func decodeDetail(kind Kind, which int, raw json.RawMessage) (any, error) {
	t := detailTypes[kind][which]
	if t == nil || len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	v := reflect.New(t)
	if err := json.Unmarshal(raw, v.Interface()); err != nil {
		return nil, fmt.Errorf("the %s detail: %w", kind, err)
	}
	return v.Elem().Interface(), nil
}

func (p *Plan) UnmarshalJSON(b []byte) error {
	type plan Plan
	v := struct {
		*plan
		Detail json.RawMessage `json:"detail"`
	}{plan: (*plan)(p)}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	var err error
	p.Detail, err = decodeDetail(p.Kind, 0, v.Detail)
	return err
}

func (r *OpResult) UnmarshalJSON(b []byte) error {
	type result OpResult
	v := struct {
		*result
		Detail json.RawMessage `json:"detail"`
	}{result: (*result)(r)}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	var err error
	r.Detail, err = decodeDetail(r.Kind, 1, v.Detail)
	return err
}
