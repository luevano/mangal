package cmd

import "testing"

func TestAnilistSecretFlagDeprecated(t *testing.T) {
	if f := anilistLoginCmd.Flags().Lookup("secret"); f == nil || f.Deprecated == "" || !f.Hidden {
		t.Errorf("--secret %+v", f)
	}
}
