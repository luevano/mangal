package oauth

import (
	"strings"
	"testing"
)

// the implicit grant's token mustn't stay in the browser history
func TestResendPageKeepsTokenOutOfHistory(t *testing.T) {
	if strings.Contains(resendFragmentAsParam, "location.href =") || !strings.Contains(resendFragmentAsParam, "history.replaceState") {
		t.Error(resendFragmentAsParam)
	}
}
