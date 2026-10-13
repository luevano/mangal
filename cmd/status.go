package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/luevano/libmangal/progress"
	"github.com/luevano/mangal/v5/library"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	// statusDelay is how long a command runs before its status line shows, so quick runs
	// stay clean.
	statusDelay = 400 * time.Millisecond
	// statusRefresh is how often the status line is redrawn.
	statusRefresh = 100 * time.Millisecond
)

// status tells what a long command is doing on stderr: on a terminal one line, rewritten
// in place once the command runs longer than statusDelay and cleared at the end; with
// --verbose every status as a line; else nothing. stdout is left alone.
type status struct {
	w     io.Writer
	width int
	// tty: the transient line, verbose: the lines (wins over tty)
	tty, verbose bool

	mu sync.Mutex
	// line is the latest status, drawn the one on screen, last the last printed line
	line, drawn, last string
	stop, done        chan struct{}
	once              sync.Once
}

func newStatus(cmd *cobra.Command) *status {
	s := &status{w: cmd.ErrOrStderr(), verbose: rootArgs.Verbose}
	if f, ok := s.w.(*os.File); ok && !s.verbose && term.IsTerminal(int(f.Fd())) {
		s.tty = true
		if width, _, err := term.GetSize(int(f.Fd())); err == nil {
			s.width = width
		}
		s.stop, s.done = make(chan struct{}), make(chan struct{})
		go s.loop()
	}
	return s
}

func (s *status) loop() {
	defer close(s.done)
	delay := time.NewTimer(statusDelay)
	defer delay.Stop()
	select {
	case <-delay.C:
	case <-s.stop:
		return
	}
	tick := time.NewTicker(statusRefresh)
	defer tick.Stop()
	for {
		s.mu.Lock()
		s.redraw()
		s.mu.Unlock()
		select {
		case <-tick.C:
		case <-s.stop:
			return
		}
	}
}

// redraw shows the latest status, called with mu held.
func (s *status) redraw() {
	if s.line == s.drawn {
		return
	}
	line := s.line
	if s.width > 1 {
		// a wrapped line couldn't be rewritten
		line = ansi.Truncate(line, s.width-1, "…")
	}
	fmt.Fprint(s.w, "\r"+ansi.EraseEntireLine+line)
	s.drawn = s.line
}

// erase clears the line on screen, called with mu held.
func (s *status) erase() {
	if s.drawn != "" {
		fmt.Fprint(s.w, "\r"+ansi.EraseEntireLine)
		s.drawn = ""
	}
}

// Set makes msg the status.
func (s *status) Set(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.verbose && msg != s.last:
		fmt.Fprintln(s.w, msg)
		s.last = msg
	case s.tty:
		s.line = msg
	}
}

// Transient makes msg the status on a terminal only, not a --verbose line (e.g. a page
// count).
func (s *status) Transient(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tty {
		s.line = msg
	}
}

// Progress makes the library's progress the status.
func (s *status) Progress(p library.Progress) {
	s.Set(p.String())
}

// Context is ctx with the libmangal progress reports (e.g. rate-limit waits) as status.
func (s *status) Context(ctx context.Context) context.Context {
	return progress.WithReporter(ctx, func(st progress.Status) { s.Set(st.String()) })
}

// Print runs print (writing to stdout) with the status line out of the way.
func (s *status) Print(print func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	drawn := s.drawn
	s.erase()
	print()
	if drawn != "" {
		s.redraw()
	}
}

// Clear removes the status until the next Set, e.g. before a question.
func (s *status) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.erase()
	s.line = ""
}

// Done clears the status for good.
func (s *status) Done() {
	s.once.Do(func() {
		if s.stop != nil {
			close(s.stop)
			<-s.done
		}
		s.Clear()
	})
}
