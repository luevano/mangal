package cmd

import (
	"net"
	"strconv"
	"testing"

	"github.com/luevano/mangal/v5/auth/oauth"
	"github.com/luevano/mangal/v5/config"
)

// the web UI and the logins' callback server can run at the same time
func TestWebPortIsNotTheCallbackPort(t *testing.T) {
	_, callback, _ := net.SplitHostPort(oauth.ServerAddr)
	if port := strconv.Itoa(config.Serve.Port.Default); port == callback {
		t.Errorf("web port %s is the callback port", port)
	}
}

func TestWebFlagsSetTheConfig(t *testing.T) {
	for name, key := range map[string]string{"host": config.Serve.Host.Key, "port": config.Serve.Port.Key} {
		if f := webCmd.Flags().Lookup(name); f == nil || len(f.Annotations[configKeyAnnotation]) != 1 || f.Annotations[configKeyAnnotation][0] != key {
			t.Errorf("--%s doesn't set %s", name, key)
		}
	}
}
