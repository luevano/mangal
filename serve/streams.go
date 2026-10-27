package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/luevano/mangal/v5/web"
)

// swapped in tests
var (
	// streamLife is how long an event stream runs before the server ends it: the client
	// connects again at once from its last event (nothing is missed), its login checked
	// again. A backstop: a login ending or its permissions changing end it at once.
	streamLife = 15 * time.Minute
	// streamsPerIdentity is how many event streams a user or a token may have open.
	streamsPerIdentity = 20
)

// Why a stream ended, in the request log.
var (
	errStreamRenewed = errors.New("renewed after 15m")
	errLoginEnded    = errors.New("closed: the login ended")
	errPermsChanged  = errors.New("closed: its permissions changed")
)

// streams are the open event streams, with who opened them: when the users or tokens
// change (authChanged) each one's login is checked again.
type streams struct {
	mu   sync.Mutex
	open map[*stream]struct{}
}

type stream struct {
	id  identity
	r   *http.Request
	end context.CancelCauseFunc
}

// add registers the stream of the request r (ctx: its handler's, with the identity): its
// ctx ends when it has to close (the cause says why), done unregisters it. Refused past
// streamsPerIdentity.
func (s *streams) add(ctx context.Context, r *http.Request) (context.Context, func(), error) {
	id, _ := identityOf(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for st := range s.open {
		if st.id.Kind == id.Kind && st.id.Name == id.Name {
			n++
		}
	}
	if n >= streamsPerIdentity {
		return nil, nil, newAPIError(http.StatusTooManyRequests, CodeRateLimited, "%s has %d event streams open already", id, n)
	}
	ctx, end := context.WithCancelCause(ctx)
	st := &stream{id: id, r: r, end: end}
	if s.open == nil {
		s.open = map[*stream]struct{}{}
	}
	s.open[st] = struct{}{}
	return ctx, func() {
		s.mu.Lock()
		delete(s.open, st)
		s.mu.Unlock()
		end(nil)
	}, nil
}

// recheck ends the streams whose login ended or whose permissions changed (the client
// connects again: refused, or with the new permissions' topics).
func (s *streams) recheck(auth *Auth) {
	s.mu.Lock()
	list := slices.Collect(maps.Keys(s.open))
	s.mu.Unlock()
	for _, st := range list {
		id, _, err := auth.identify(st.r)
		switch {
		case err != nil:
			st.end(errLoginEnded)
		case !slices.Equal(id.Scopes, st.id.Scopes):
			st.end(errPermsChanged)
		}
	}
}

// authChanged applies a change of the users, sessions or tokens: this server's or another
// process's (the database watcher).
func (a *API) authChanged() {
	a.Auth.Forget()
	a.streams.recheck(a.Auth)
}

// writeStream writes the events as server-sent events, a ping every eventsPing, until
// ctx ends, events closes (a subscriber too far behind connects again from its last
// event) or the stream's life is over; why it ended goes to the request log.
// seqOf is an event's id (the seq), false to leave it out.
func writeStream[E any](ctx context.Context, r *http.Request, w http.ResponseWriter, events <-chan E, seqOf func(E) (uint64, bool)) {
	rc := http.NewResponseController(w)
	startStream(w)
	if _, err := io.WriteString(w, ": mangal serve events\n\n"); err != nil {
		return
	}
	_ = rc.Flush()
	ping := time.NewTicker(eventsPing)
	defer ping.Stop()
	life := time.NewTimer(streamLife)
	defer life.Stop()
	for {
		select {
		case <-ctx.Done():
			if cause := context.Cause(ctx); errors.Is(cause, errLoginEnded) || errors.Is(cause, errPermsChanged) {
				web.LogRefused(r, cause.Error())
			}
			return
		case <-life.C:
			web.LogRefused(r, errStreamRenewed.Error())
			return
		case <-ping.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
		case e, ok := <-events:
			if !ok {
				return
			}
			seq, ok := seqOf(e)
			if !ok {
				continue
			}
			b, err := json.Marshal(e)
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", seq, b); err != nil {
				return
			}
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}
