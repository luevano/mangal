package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/config"
)

func TestConfigInfoMasksSecrets(t *testing.T) {
	if err := config.Set(config.Notification.Discord.WebhookURL.Key, "https://discord.com/api/webhooks/1/s3cret-token"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.Set(config.Notification.Discord.WebhookURL.Key, "") })
	for _, json := range []bool{false, true} {
		configInfoArgs.JSON = json
		var out bytes.Buffer
		configInfoCmd.SetOut(&out)
		if err := configInfoCmd.RunE(configInfoCmd, nil); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "s3cret-token") || !strings.Contains(out.String(), "notification.discord.webhook_url") {
			t.Errorf("json %t: the token is shown", json)
		}
	}
	configInfoArgs.JSON = false
}
