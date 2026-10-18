package manager

import (
	"context"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
)

type fakeLoader libmangal.ProviderInfo

func (l fakeLoader) String() string                                 { return "loader " + l.Name }
func (l fakeLoader) Info() libmangal.ProviderInfo                   { return libmangal.ProviderInfo(l) }
func (fakeLoader) Load(context.Context) (libmangal.Provider, error) { return nil, nil }

func TestDedupe(t *testing.T) {
	all := []libmangal.ProviderLoader{
		fakeLoader{ID: "a", Name: "1", History: []libmangal.ProviderAlias{{ID: "old"}}},
		fakeLoader{ID: "a", Name: "2"},
		fakeLoader{ID: "b", Name: "3", History: []libmangal.ProviderAlias{{ID: "old"}}},
		fakeLoader{ID: "old", Name: "4"},
		fakeLoader{ID: "c", Name: "5"},
	}
	loaders, problems, err := dedupe(all, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, l := range loaders {
		names = append(names, l.Info().Name)
	}
	if strings.Join(names, ",") != "1,5" || len(problems) != 3 {
		t.Errorf("kept %v, problems %v", names, problems)
	}
	if !strings.Contains(problems[0].Error(), `loader 2: duplicate id "a"`) {
		t.Errorf("problem %q", problems[0])
	}
}
