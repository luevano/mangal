package core

import "testing"

// every change kind is a feature's, every feature is in each mode or says why not
func TestFeatures(t *testing.T) {
	for _, k := range Kinds {
		if _, ok := FeatureOf(k); !ok {
			t.Errorf("change %s is no feature's", k)
		}
	}
	names := map[string]bool{}
	for _, f := range Features {
		if names[f.Name] {
			t.Errorf("%s twice", f.Name)
		}
		names[f.Name] = true
		for _, m := range []Mode{ModeCLI, ModeTUI, ModeScript} {
			if !f.Has(m) && f.NotApplicable[m] == "" {
				t.Errorf("%s: not in %s, and no reason", f.Name, m)
			}
			if f.Has(m) && f.NotApplicable[m] != "" {
				t.Errorf("%s: in %s and not applicable", f.Name, m)
			}
		}
	}
}
