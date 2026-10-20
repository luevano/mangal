package base

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/progress"
	"github.com/luevano/mangal/v5/core"
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

type loadLabelMsg struct {
	id    int64
	label string
	next  tea.Cmd
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
	return load(ctx, loadIDs.Add(1), label, work, done, func() {})
}

// LoadWithStatus is Load whose label follows the work: status(label) replaces it while
// it runs (the latest wins when they come faster than they're shown). The ctx of work
// carries libmangal's progress reports (e.g. rate-limit waits) to the label too.
func LoadWithStatus[T any](ctx context.Context, label string, work func(ctx context.Context, status func(string)) (T, error), done func(T) tea.Msg) tea.Cmd {
	id := loadIDs.Add(1)
	feed := &labelFeed{ch: make(chan string, 1)}
	run := func(ctx context.Context) (T, error) {
		defer feed.close()
		ctx = progress.WithReporter(ctx, func(s progress.Status) { feed.send(s.String()) })
		return work(ctx, feed.send)
	}
	return tea.Batch(load(ctx, id, label, run, done, feed.close), feed.listen(id))
}

// labelFeed passes the labels of a LoadWithStatus to the TUI, only the latest is kept.
type labelFeed struct {
	mu     sync.Mutex
	ch     chan string
	closed bool
}

func (f *labelFeed) send(label string) {
	// the TUI's labels start with a capital, library statuses don't
	label = strings.TrimSpace(label)
	if r, size := utf8.DecodeRuneInString(label); r != utf8.RuneError {
		label = string(unicode.ToUpper(r)) + label[size:]
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	select {
	case <-f.ch:
	default:
	}
	f.ch <- label
}

func (f *labelFeed) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.ch)
	}
}

func (f *labelFeed) listen(id int64) tea.Cmd {
	return func() tea.Msg {
		label, ok := <-f.ch
		if !ok {
			return nil
		}
		return loadLabelMsg{id: id, label: label, next: f.listen(id)}
	}
}

// load is Load with the id, skipped is called when work won't run (the TUI is quitting).
func load[T any](ctx context.Context, id int64, label string, work func(context.Context) (T, error), done func(T) tea.Msg, skipped func()) tea.Cmd {
	sc := scopeOf(ctx)
	if sc != nil {
		sc.busy.Add(1)
	}
	return tea.Sequence(
		func() tea.Msg { return loadStartMsg{id: id, label: label} },
		func() tea.Msg {
			release, ok := Track(ctx)
			if !ok {
				skipped()
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

// MangaGone is the error for a manga the provider doesn't have (any more), matching
// watch.ErrMangaNotFound.
func MangaGone(title, provider, id string) error {
	return core.MangaGone(title, provider, id)
}

// MangaError is err from getting the provider's manga's volumes or chapters, MangaGone
// when the provider answered it doesn't have the manga (libmangal.ErrNotFound).
func MangaError(err error, provider string, manga mangadata.Manga) error {
	if errors.Is(err, libmangal.ErrNotFound) {
		info := manga.Info()
		return MangaGone(info.Title, provider, info.ID)
	}
	return err
}
