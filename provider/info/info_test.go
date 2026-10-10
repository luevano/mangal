package info

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
)

func TestNew(t *testing.T) {
	type args struct {
		r io.Reader
	}
	tests := []struct {
		name     string
		args     args
		wantInfo Info
		wantErr  bool
	}{
		{
			name: "bundle",
			args: args{
				r: strings.NewReader(`
type = "lua"
id = "some-id"
`),
			},
			wantInfo: Info{
				ProviderInfo: libmangal.ProviderInfo{
					ID: "some-id",
				},
				Type: TypeLua,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotInfo, err := New(tt.args.r)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotInfo, tt.wantInfo) {
				t.Errorf("New() gotInfo = %v, want %v", gotInfo, tt.wantInfo)
			}
		})
	}
}

func TestNewHistory(t *testing.T) {
	toml := `
type = "lua"
id = "weebcentral"
name = "Weeb Central"
version = "0.4.0"
website = "https://weebcentral.com/"

[[history]]
id = "mangasee"
name = "MangaSee"
website = "https://mangasee123.com/"
`
	info, err := New(strings.NewReader(toml))
	if err != nil {
		t.Fatal(err)
	}
	want := []libmangal.ProviderAlias{{ID: "mangasee", Name: "MangaSee", Website: "https://mangasee123.com/"}}
	if !reflect.DeepEqual(info.History, want) {
		t.Errorf("got %+v, want %+v", info.History, want)
	}
	if !info.Matches("mangasee") {
		t.Error("doesn't match its old id")
	}
}
