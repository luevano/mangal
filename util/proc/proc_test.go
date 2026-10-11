package proc

import (
	"os"
	"os/exec"
	"testing"
)

func TestAlive(t *testing.T) {
	if !Alive(os.Getpid()) {
		t.Error("this process isn't alive")
	}
	if Alive(0) || Alive(-1) {
		t.Error("invalid pids are alive")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Skip(err)
	}
	if Alive(cmd.Process.Pid) {
		t.Errorf("exited process %d is alive", cmd.Process.Pid)
	}
}
