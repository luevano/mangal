package anilist

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal/mangadata"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/tui/base"
)

// fakeSource answers after wait is closed (never, if nil, until its ctx is done).
type fakeSource struct {
	client.MetadataSearcher
	code lmmeta.IDCode
	wait chan struct{}
	err  error
}

func (f *fakeSource) FindClosestContext(ctx context.Context, q lmmeta.Query) (lmmeta.Match, bool, error) {
	if f.wait != nil {
		select {
		case <-f.wait:
		case <-ctx.Done():
			return lmmeta.Match{}, false, ctx.Err()
		}
	}
	if f.err != nil {
		return lmmeta.Match{}, false, f.err
	}
	m := &mangadata.Metadata{EnglishTitle: q.Titles[0], ProviderID: "1", ProviderIDCode: string(f.code)}
	return lmmeta.Match{Metadata: m, Score: 1}, true, nil
}

func (f *fakeSource) SearchContext(context.Context, string) ([]lmmeta.Metadata, error) {
	return nil, nil
}

// startSearch runs the search's cmds at once as the TUI does, the results go to the channel.
func startSearch(t *testing.T, s *state, ctx context.Context, query string) <-chan sourceResultsMsg {
	t.Helper()
	batch, ok := s.searchCmd(ctx, query)().(tea.BatchMsg)
	if !ok {
		t.Fatal("not a batch")
	}
	results := make(chan sourceResultsMsg, len(batch))
	for _, cmd := range batch {
		go func() {
			for _, msg := range base.Drain(cmd) {
				if m, ok := msg.(sourceResultsMsg); ok {
					results <- m
				}
			}
		}()
	}
	return results
}

func next(t *testing.T, results <-chan sourceResultsMsg) sourceResultsMsg {
	t.Helper()
	select {
	case m := <-results:
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("no results")
		return sourceResultsMsg{}
	}
}

// a slow source doesn't delay a fast one, a failing one only fails itself
func TestSearchSourcesArriveApart(t *testing.T) {
	al, kt, mu := client.MetadataSources[0], client.MetadataSources[1], client.MetadataSources[3]
	release := make(chan struct{})
	s := newState([]searchSource{
		{src: mu, provider: &fakeSource{code: mu.Code, wait: release}},
		{src: al, provider: &fakeSource{code: al.Code}},
		{src: kt, provider: &fakeSource{code: kt.Code, err: errors.New("kitsu is down")}},
	}, "frieren")
	ctx := context.Background()
	results := startSearch(t, s, ctx, "frieren")
	if got := s.Subtitle(); strings.Count(got, searching) != 3 {
		t.Errorf("subtitle %q", got)
	}

	for range 2 {
		s.Update(ctx, next(t, results))
	}
	if got := len(s.List.Items()); got != 1 {
		t.Errorf("%d results with MangaUpdates waiting, want AniList's", got)
	}
	if got := s.Subtitle(); !strings.Contains(got, "MangaUpdates "+searching) || !strings.Contains(got, "AniList 1") || !strings.Contains(got, "Kitsu failed") {
		t.Errorf("subtitle %q", got)
	}

	close(release)
	s.Update(ctx, next(t, results))
	if got := len(s.List.Items()); got != 2 {
		t.Errorf("%d results, want 2", got)
	}
	if first := s.List.Items()[0].(*item); first.src.Code != al.Code {
		t.Errorf("first is %s: the results should be in the order they came", first.src.Code)
	}
	if got := s.Subtitle(); strings.Contains(got, searching) {
		t.Errorf("subtitle %q", got)
	}
}

// leaving stops the sources, a new search stops the one before and drops its results
func TestSearchCancels(t *testing.T) {
	al := client.MetadataSources[0]
	block := make(chan struct{})
	defer close(block)
	s := newState([]searchSource{
		{src: al, provider: &fakeSource{code: al.Code, wait: block}},
		{src: client.MetadataSources[1], provider: &fakeSource{code: client.MetadataSources[1].Code, wait: block}},
	}, "x")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	old := startSearch(t, s, ctx, "old")
	newer := startSearch(t, s, ctx, "new")
	for range 2 {
		m := next(t, old)
		if !errors.Is(m.err, context.Canceled) {
			t.Errorf("old search: %v", m.err)
		}
		s.Update(ctx, m)
	}
	if got := s.Subtitle(); strings.Count(got, searching) != 2 {
		t.Errorf("the old search's results changed the new one: %q", got)
	}

	cancel()
	for range 2 {
		if m := next(t, newer); !errors.Is(m.err, context.Canceled) {
			t.Errorf("left: %v", m.err)
		}
	}
}
