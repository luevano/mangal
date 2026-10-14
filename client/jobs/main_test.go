package jobs

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// the tests' providers aren't installed ones: no provider loading
	resolveProvider = func(id string) string { return id }
	os.Exit(m.Run())
}
