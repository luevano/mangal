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

func TestFormatFloa32(t *testing.T) {
	for n, want := range map[float32]string{10.1: "10.1", 1.5: "1.5", 3: "3", 0.25: "0.25"} {
		if got := FormatFloa32(n); got != want {
			t.Errorf("FormatFloa32(%v) = %q, want %q", n, got, want)
		}
	}
}
