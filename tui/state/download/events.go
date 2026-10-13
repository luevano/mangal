package download

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal"
	lmprogress "github.com/luevano/libmangal/progress"
	"github.com/luevano/mangal/v5/client"
)

// EventMsg is an event of a download (a Feed's), with a copy of the chapter's result.
type EventMsg struct {
	Run     int
	Kind    client.EventKind
	Index   int
	Result  client.Result
	Attempt int
	Wait    time.Duration
	Busy    *client.ProviderBusy
	// Progress is set for client.EventProgress, Status for EventStatus
	Progress *libmangal.DownloadProgress
	Status   string
	// Next waits for the following event
	Next tea.Cmd
}

// EventStatus is a provider's status (libmangal's progress) during the download.
const EventStatus client.EventKind = -1

// FinishedMsg is the end of a Feed: its download is over.
type FinishedMsg struct{ Run int }

// Feed passes the events of a download running in the background to the State, in
// order: the download Sends them, Listen gives them as EventMsgs (FinishedMsg once it's
// Closed). Run tells the downloads of a State apart.
type Feed struct {
	run int
	ctx context.Context
	ch  chan EventMsg
}

// NewFeed is a feed for the download of run, ctx is the State's.
func NewFeed(ctx context.Context, run int) *Feed {
	return &Feed{run: run, ctx: ctx, ch: make(chan EventMsg, 8)}
}

// Send passes e on, it waits for the State to take it (or ctx).
func (f *Feed) Send(e client.Event) {
	msg := EventMsg{Run: f.run, Kind: e.Kind, Index: e.Index, Attempt: e.Attempt, Wait: e.Wait, Busy: e.Busy, Progress: e.Progress}
	if e.Result != nil {
		msg.Result = *e.Result
	}
	f.send(msg)
}

// Reporting is ctx with libmangal's progress reports passed on as EventStatus.
func (f *Feed) Reporting(ctx context.Context) context.Context {
	return lmprogress.WithReporter(ctx, func(st lmprogress.Status) {
		f.send(EventMsg{Run: f.run, Kind: EventStatus, Status: st.String()})
	})
}

func (f *Feed) send(msg EventMsg) {
	select {
	case f.ch <- msg:
	case <-f.ctx.Done():
	}
}

// Close ends the feed, once the download is over.
func (f *Feed) Close() { close(f.ch) }

// Listen waits for the next event.
func (f *Feed) Listen() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-f.ch
		if !ok {
			return FinishedMsg{Run: f.run}
		}
		msg.Next = f.Listen()
		return msg
	}
}
