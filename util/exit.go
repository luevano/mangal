package util

import (
	"os"
	"sync"
)

var (
	exitMu    sync.Mutex
	exitHooks []func()
)

// OnExit registers f to run when mangal exits, through Exit or RunExitHooks.
func OnExit(f func()) {
	exitMu.Lock()
	defer exitMu.Unlock()
	exitHooks = append(exitHooks, f)
}

// RunExitHooks runs the registered hooks once, the last registered first.
func RunExitHooks() {
	exitMu.Lock()
	hooks := exitHooks
	exitHooks = nil
	exitMu.Unlock()
	for i := len(hooks) - 1; i >= 0; i-- {
		hooks[i]()
	}
}

// Exit runs the exit hooks and exits with the code.
func Exit(code int) {
	RunExitHooks()
	os.Exit(code)
}
