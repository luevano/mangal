//go:build unix

package proc

import (
	"errors"
	"syscall"
)

// Alive is true when a process with the pid runs (on this host).
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	// EPERM: it runs as another user
	return err == nil || errors.Is(err, syscall.EPERM)
}
