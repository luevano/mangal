package cmd

import (
	"net"
	"testing"

	"github.com/luevano/mangal/v5/auth/oauth"
)

// the web UI and the logins' callback server can run at the same time
func TestWebPortIsNotTheCallbackPort(t *testing.T) {
	_, callback, _ := net.SplitHostPort(oauth.ServerAddr)
	if port := webCmd.Flags().Lookup("port").DefValue; port == callback {
		t.Errorf("web port %s is the callback port", port)
	}
}
