package util

import "slices"

import "sync"

var (
	exitMu    sync.Mutex
	exitHooks []func()
)

// OnExit registers f to run when mangal exits (RunExitHooks).
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
	for _, hook := range slices.Backward(hooks) {
		hook()
	}
}
