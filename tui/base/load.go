package base

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

var loadIDs atomic.Int64

type loadStartMsg struct {
	id    int64
	label string
}

type loadDoneMsg struct {
	id  int64
	ctx context.Context
	msg tea.Msg
}

// Load runs work in the background, with label and a spinner shown meanwhile (none if
// label is empty). Its result reaches the State as done(v), or as the error. done only
// builds the msg (e.g. a typed result msg, or a State to push): the State applies it in
// Update, so nothing is changed from the background.
//
// ctx is the one the State got in Init or Update. The result is dropped when ctx is done
// by the time it arrives: the State isn't shown anymore, or was left and shown again.
// Overlapping loads each keep their label until they finish.
func Load[T any](ctx context.Context, label string, work func(context.Context) (T, error), done func(T) tea.Msg) tea.Cmd {
	id := loadIDs.Add(1)
	sc := scopeOf(ctx)
	if sc != nil {
		sc.busy.Add(1)
	}
	return tea.Sequence(
		func() tea.Msg { return loadStartMsg{id: id, label: label} },
		func() tea.Msg {
			release, ok := Track(ctx)
			if !ok {
				return loadDoneMsg{id: id, ctx: ctx}
			}
			defer release()
			v, err := work(ctx)
			if err != nil {
				return loadDoneMsg{id: id, ctx: ctx, msg: err}
			}
			var msg tea.Msg
			if done != nil {
				msg = done(v)
			}
			return loadDoneMsg{id: id, ctx: ctx, msg: msg}
		},
	)
}

// Busy reports whether a Load started with ctx (the State's current one) is still
// running, to ignore a second enter while the first one loads.
func Busy(ctx context.Context) bool {
	sc := scopeOf(ctx)
	return sc != nil && sc.busy.Load() > 0
}

// Track registers background work that doesn't go through Load (e.g. a download), so
// quitting waits a moment for it to stop once ctx is cancelled. Call release when done.
// ok is false when the TUI is quitting, the work shouldn't start then.
func Track(ctx context.Context) (release func(), ok bool) {
	sc := scopeOf(ctx)
	if sc == nil || sc.work == nil {
		return func() {}, true
	}
	return sc.work.add()
}

// Background is a ctx for work that outlives the State that starts it (e.g. a watch
// check left running while another screen is shown): cancelled only when the TUI quits.
// Track it as any other work.
func Background(ctx context.Context) context.Context {
	sc := scopeOf(ctx)
	if sc == nil || sc.root == nil {
		return context.WithoutCancel(ctx)
	}
	return context.WithValue(sc.root, scopeKey{}, &scope{work: sc.work, root: sc.root})
}

type scopeKey struct{}

// scope is what base puts in the ctx of each State.
type scope struct {
	// busy counts the running loads
	busy atomic.Int32
	work *tracker
	// root is cancelled on quit
	root context.Context
}

func scopeOf(ctx context.Context) *scope {
	sc, _ := ctx.Value(scopeKey{}).(*scope)
	return sc
}

// tracker counts the running background work, to wait for it on quit.
type tracker struct {
	mu      sync.Mutex
	running int
	closed  bool
	idle    chan struct{}
}

func (t *tracker) add() (func(), bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return func() {}, false
	}
	t.running++
	var once sync.Once
	return func() { once.Do(t.done) }, true
}

func (t *tracker) done() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.running--
	if t.running == 0 && t.idle != nil {
		close(t.idle)
		t.idle = nil
	}
}

// wait stops new work and waits for the running one, up to timeout.
func (t *tracker) wait(timeout time.Duration) bool {
	t.mu.Lock()
	t.closed = true
	if t.running == 0 {
		t.mu.Unlock()
		return true
	}
	if t.idle == nil {
		t.idle = make(chan struct{})
	}
	idle := t.idle
	t.mu.Unlock()

	select {
	case <-idle:
		return true
	case <-time.After(timeout):
		return false
	}
}
