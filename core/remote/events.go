package remote

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/luevano/mangal/v5/web/apiv1"
)

// Event is an event of the server's /api/v1/events.
type Event struct {
	Seq     uint64          `json:"seq"`
	Topic   string          `json:"topic"`
	Kind    string          `json:"kind"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// reconnectAfter is the pause before a dropped event stream is opened again.
var reconnectAfter = 2 * time.Second

// Events tells fn the server's events of the topics after the one with Seq since (0: from
// now), until ctx is done: nil then. A dropped stream is opened again from the last event;
// the error of the first connection, or of one the server refuses (a token not taken any
// more), is returned.
func (c *Client) Events(ctx context.Context, topics []string, since uint64, fn func(Event)) error {
	resp, err := c.events(ctx, topics, since)
	for {
		if err != nil {
			var e *Error
			if ctx.Err() != nil || (errors.As(err, &e) && e.Status < 500) {
				return ignoreDone(ctx, err)
			}
		} else {
			since = readStream(ctx, resp.Body, since, fn)
			resp.Body.Close()
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(reconnectAfter):
		}
		resp, err = c.events(ctx, topics, since)
	}
}

func ignoreDone(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// events opens the stream: the client's transport and redirect rule, no timeout.
func (c *Client) events(ctx context.Context, topics []string, since uint64) (*http.Response, error) {
	q := url.Values{"topics": {strings.Join(topics, ",")}}
	if since > 0 {
		q.Set("since", strconv.FormatUint(since, 10))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+"/api/v1/events?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	if err := c.edit(ctx, req); err != nil {
		return nil, err
	}
	stream := &http.Client{Transport: c.HTTP.Transport, CheckRedirect: c.HTTP.CheckRedirect}
	resp, err := stream.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		var e apiv1.Error
		if json.Unmarshal(body, &e) != nil {
			return nil, errorOf(resp.StatusCode, body, nil)
		}
		return nil, errorOf(resp.StatusCode, body, &e)
	}
	return resp, nil
}

// readStream tells fn the stream's events until it ends, returns the last one's seq.
func readStream(ctx context.Context, body io.Reader, last uint64, fn func(Event)) uint64 {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	var id uint64
	for sc.Scan() && ctx.Err() == nil {
		if v, ok := strings.CutPrefix(sc.Text(), "id:"); ok {
			id, _ = strconv.ParseUint(strings.TrimSpace(v), 10, 64)
			continue
		}
		data, ok := strings.CutPrefix(sc.Text(), "data:")
		if !ok {
			// keep-alives, the blank lines
			continue
		}
		var e Event
		if json.Unmarshal([]byte(strings.TrimSpace(data)), &e) != nil {
			continue
		}
		last = e.Seq
		fn(e)
	}
	if errors.Is(sc.Err(), bufio.ErrTooLong) && id > last {
		// skipped: the server would send it again on every reconnection
		last = id
	}
	return last
}
