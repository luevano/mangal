package core_test

import (
	"testing"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/web"
)

// every API v1 operation is a feature's (or the API's own), every feature's operation is
// in the spec
func TestFeaturesAPI(t *testing.T) {
	ops, err := web.Operations()
	if err != nil {
		t.Fatal(err)
	}
	inSpec := map[string]bool{}
	for _, o := range ops {
		if !o.V1 {
			continue
		}
		inSpec[o.ID] = true
		_, own := core.APIOperations[o.ID]
		if _, ok := core.FeatureOfOperation(o.ID); !ok && !own {
			t.Errorf("operation %s is no feature's", o.ID)
		}
	}
	for _, f := range core.Features {
		for _, id := range f.API {
			if !inSpec[id] {
				t.Errorf("%s: operation %s isn't in the spec", f.Name, id)
			}
		}
	}
	for id := range core.APIOperations {
		if !inSpec[id] {
			t.Errorf("operation %s isn't in the spec", id)
		}
	}
}
