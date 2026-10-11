//go:build !unix

package proc

import "os"

// Alive is true when a process with the pid runs (on this host).
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	// windows: fails when there's no such process
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p.Release()
	return true
}
