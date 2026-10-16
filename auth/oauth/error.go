package oauth

import "errors"

var (
	ErrEmptyClientID    = errors.New("oauth: empty client id")
	ErrEmptyCode        = errors.New("oauth: empty access code or token")
	ErrNilToken         = errors.New("oauth: no token")
	ErrTooManyCallbacks = errors.New("oauth: too many callback requests")
	ErrInvalidState     = errors.New("oauth: invalid callback state")
	ErrTimeout          = errors.New("oauth: timed out waiting for the login")
	// ErrNeedsPaste is a login that can only finish with a pasted code (e.g. AniList's pin).
	ErrNeedsPaste = errors.New("oauth: the login needs the code pasted")
)
