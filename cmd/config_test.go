package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/config"
)

func TestConfigInfoMasksSecrets(t *testing.T) {
	if err := config.Set(config.Serve.Token.Key, "s3cret-token"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.Set(config.Serve.Token.Key, "") })
	for _, json := range []bool{false, true} {
		configInfoArgs.JSON = json
		var out bytes.Buffer
		configInfoCmd.SetOut(&out)
		if err := configInfoCmd.RunE(configInfoCmd, nil); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "s3cret-token") || !strings.Contains(out.String(), "serve.token") {
			t.Errorf("json %t: the token is shown", json)
		}
	}
	configInfoArgs.JSON = false
}
