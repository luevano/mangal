package oauth

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestParsePaste(t *testing.T) {
	const state = "s1"
	tests := []struct {
		name     string
		input    string
		implicit bool
		want     pasted
		err      error
	}{
		{name: "callback url", input: " http://localhost:6969/oauth/mal/callback?code=abc&state=s1\n", want: pasted{code: "abc"}},
		{name: "query only", input: "?code=abc&state=s1", want: pasted{code: "abc"}},
		{name: "bare code", input: "def502abc", want: pasted{code: "def502abc"}},
		{name: "no state", input: "http://localhost:6969/oauth/al/callback?code=abc", want: pasted{code: "abc"}},
		{name: "other state", input: "http://localhost:6969/oauth/al/callback?code=abc&state=s2", err: ErrInvalidState},
		{name: "denied", input: "http://localhost:6969/oauth/mal/callback?error=access_denied&state=s1", err: errAny},
		{name: "no code", input: "http://localhost:6969/oauth/mal/callback?state=s1", err: ErrBadPaste},
		{name: "empty", input: "  ", err: ErrBadPaste},
		{name: "sentence", input: "it didn't work", err: ErrBadPaste},
		{
			name: "implicit fragment", implicit: true,
			input: "http://localhost:6969/oauth/al/callback#access_token=eyJ.a.b&token_type=Bearer&expires_in=31536000",
			want:  pasted{token: "eyJ.a.b", expiresIn: 31536000 * time.Second},
		},
		{name: "implicit fragment only", implicit: true, input: "#access_token=eyJ.a.b&state=s1", want: pasted{token: "eyJ.a.b"}},
		{name: "implicit bare token", implicit: true, input: "eyJ.a.b", want: pasted{token: "eyJ.a.b"}},
		{name: "implicit without token", implicit: true, input: "http://localhost:6969/oauth/al/callback?code=abc", err: ErrBadPaste},
		{name: "implicit other state", implicit: true, input: "#access_token=x&state=s2", err: ErrInvalidState},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePaste(tt.input, state, tt.implicit)
			switch {
			case tt.err == errAny:
				if err == nil {
					t.Fatal("no error")
				}
			case tt.err != nil:
				if !errors.Is(err, tt.err) {
					t.Fatalf("got %v, want %v", err, tt.err)
				}
			case err != nil:
				t.Fatal(err)
			case got != tt.want:
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

var errAny = errors.New("any error")

func TestAnilistManualImplicit(t *testing.T) {
	o, err := NewAnilistLoginOption("id", "")
	if err != nil {
		t.Fatal(err)
	}
	o.server.listen = func() (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") }
	pasted := make(chan string, 1)
	shown := ""
	err = o.AuthorizeManual(context.Background(), Manual{
		Pasted: pasted,
		Show: func(u string) {
			shown = u
			pasted <- AnilistServerCallbackURL + "#access_token=pasted&token_type=Bearer&expires_in=3600"
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if shown == "" || o.Token().AccessToken != "pasted" || time.Until(o.Token().Expiry) > time.Hour {
		t.Errorf("shown %q, token %+v", shown, o.Token())
	}

	// without BadPaste a bad paste ends the login
	pasted <- "what?"
	if err := o.AuthorizeManual(context.Background(), Manual{Pasted: pasted, Show: func(string) {}}); !errors.Is(err, ErrBadPaste) {
		t.Errorf("got %v, want ErrBadPaste", err)
	}
}
