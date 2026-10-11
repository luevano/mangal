package util

import "testing"

func TestExitHooks(t *testing.T) {
	var got []int
	OnExit(func() { got = append(got, 1) })
	OnExit(func() { got = append(got, 2) })
	RunExitHooks()
	RunExitHooks()
	if len(got) != 2 || got[0] != 2 || got[1] != 1 {
		t.Errorf("ran %v, want [2 1] once", got)
	}
}
