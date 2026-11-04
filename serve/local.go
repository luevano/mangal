package serve

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/notify"
	"github.com/luevano/mangal/v5/web"
)

// LocalURL is the address the in-process API answers on (any would do, nothing listens).
const LocalURL = "http://localhost"

// Local is the API served in this process, without a listener or a scheduler: what scripts
// and mangal api --local use when no mangal serve runs. Every request is the script
// (KindScript) with its permissions; the downloads and jobs run in this process.
type Local struct {
	// Client sends requests to it, at LocalURL.
	Client *http.Client
	api    *API
	stop   context.CancelFunc
}

// NewLocal is the in-process API over this config's data, the script's permissions perms.
func NewLocal(ctx context.Context, perms []string) (*Local, error) {
	scopes, err := NormalizeScopes(perms)
	if err != nil {
		return nil, err
	}
	svc, err := watch.New(notify.Watches())
	if err != nil {
		return nil, err
	}
	svc.OnError = func(err error) { log.Warn("%s", err) }
	browser, err := web.NewBrowser(io.Discard)
	if err != nil {
		return nil, err
	}
	ctx, stop := context.WithCancel(ctx)
	api := &API{
		Manager: func() watch.Manager { return svc },
		Info:    watch.Status{Version: meta.Version, Started: time.Now()},
		Browser: browser,
		Jobs:    jobs.New(ctx),
		Events:  NewEvents(),
	}
	go api.Events.forwardWatches(ctx, svc)
	api.watchDB(ctx)
	return newLocal(ctx, stop, api, scopes)
}

// newLocal serves api in this process until stop.
func newLocal(ctx context.Context, stop context.CancelFunc, api *API, scopes []string) (*Local, error) {
	api.ctx = ctx
	api.Runs = &runs{}
	api.Auth = &Auth{Mode: AuthScript, ScriptScopes: scopes}
	if api.Jobs != nil {
		go api.Events.forwardJobs(ctx, api.Jobs)
	}
	app, err := api.NewApp(AppOptions{Guard: web.GuardOptions{AnyHost: true}})
	if err != nil {
		stop()
		return nil, err
	}
	return &Local{Client: &http.Client{Transport: handlerTransport{app}}, api: api, stop: stop}, nil
}

// Close waits for the jobs started here to end (cancelled when ctx is), then stops.
func (l *Local) Close(ctx context.Context) {
	if m := l.api.Jobs; m != nil {
	wait:
		for {
			changed := m.Changed()
			if len(m.Active()) == 0 {
				break
			}
			select {
			case <-changed:
			case <-ctx.Done():
				break wait
			}
		}
		m.Shutdown(10 * time.Second)
	}
	l.stop()
	l.api.Wait()
}

// Active is how many jobs started here are queued or running.
func (l *Local) Active() int {
	if l.api.Jobs == nil {
		return 0
	}
	return len(l.api.Jobs.Active())
}

// handlerTransport sends the requests to h in this process; the answer's body streams as h
// writes it (the events).
type handlerTransport struct{ h http.Handler }

func (t handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.RequestURI = req.URL.RequestURI()
	req.RemoteAddr = "127.0.0.1:0"
	if req.Body == nil {
		req.Body = http.NoBody
	}
	pr, pw := io.Pipe()
	w := &pipeWriter{header: http.Header{}, pw: pw, ready: make(chan struct{})}
	go func() {
		defer func() {
			w.WriteHeader(http.StatusOK)
			pw.Close()
		}()
		t.h.ServeHTTP(w, req)
	}()
	select {
	case <-w.ready:
	case <-req.Context().Done():
		pr.Close()
		return nil, req.Context().Err()
	}
	return &http.Response{Status: fmt.Sprintf("%d %s", w.status, http.StatusText(w.status)), StatusCode: w.status, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: w.sent, Body: pr, ContentLength: -1, Request: req}, nil
}

// pipeWriter is the ResponseWriter of handlerTransport: the body goes through the pipe.
type pipeWriter struct {
	header http.Header
	// sent is the header as written with the status
	sent   http.Header
	status int
	pw     *io.PipeWriter
	once   sync.Once
	ready  chan struct{}
}

func (w *pipeWriter) Header() http.Header { return w.header }

func (w *pipeWriter) WriteHeader(status int) {
	w.once.Do(func() {
		w.status, w.sent = status, w.header.Clone()
		close(w.ready)
	})
}

func (w *pipeWriter) Write(b []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return w.pw.Write(b)
}

func (w *pipeWriter) Flush() { w.WriteHeader(http.StatusOK) }
