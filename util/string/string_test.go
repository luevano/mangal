package string

import "testing"

func TestQuantify(t *testing.T) {
	type args struct {
		n        int
		singular string
		plural   string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "zero",
			args: args{0, "manga", "mangas"},
			want: "0 mangas",
		},
		{
			name: "one",
			args: args{1, "manga", "mangas"},
			want: "1 manga",
		},
		{
			name: "two",
			args: args{2, "manga", "mangas"},
			want: "2 mangas",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Quantify(tt.args.n, tt.args.singular, tt.args.plural); got != tt.want {
				t.Errorf("Quantify() = %v, want %v", got, tt.want)
			}
		})
	}
}
