package oauth

import (
	"errors"
	"maps"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrBadPaste is a paste that is neither the redirected address nor a code (or token).
var ErrBadPaste = errors.New("oauth: neither the redirected address nor a code")

// pasted is a parsed paste: the code of a code grant, or the token of an implicit grant.
type pasted struct {
	code      string
	token     string
	expiresIn time.Duration
}

// parsePaste reads what the user pasted: the address the browser was redirected to (or
// only its query or fragment), or the bare code (the token when implicit). A state in it
// must be the login's.
func parsePaste(input, state string, implicit bool) (pasted, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return pasted{}, ErrBadPaste
	}

	var values url.Values
	switch {
	case strings.Contains(input, "://"):
		u, err := url.Parse(input)
		if err != nil {
			return pasted{}, ErrBadPaste
		}
		values = u.Query()
		// the implicit grant's token comes in the fragment
		fragment, _ := url.ParseQuery(u.Fragment)
		maps.Copy(values, fragment)
	case strings.Contains(input, "="):
		var err error
		values, err = url.ParseQuery(strings.TrimLeft(input, "?#"))
		if err != nil {
			return pasted{}, ErrBadPaste
		}
	case strings.ContainsAny(input, " \t/?#&"):
		return pasted{}, ErrBadPaste
	case implicit:
		return pasted{token: input}, nil
	default:
		return pasted{code: input}, nil
	}

	if e := values.Get("error"); e != "" {
		return pasted{}, providerError(e, values.Get("error_description"))
	}
	if s := values.Get("state"); s != "" && s != state {
		return pasted{}, ErrInvalidState
	}
	if implicit {
		p := pasted{token: values.Get("access_token")}
		if p.token == "" {
			return pasted{}, ErrBadPaste
		}
		if secs, err := strconv.Atoi(values.Get("expires_in")); err == nil && secs > 0 {
			p.expiresIn = time.Duration(secs) * time.Second
		}
		return p, nil
	}
	p := pasted{code: values.Get("code")}
	if p.code == "" {
		return pasted{}, ErrBadPaste
	}
	return p, nil
}
