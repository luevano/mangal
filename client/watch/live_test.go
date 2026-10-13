package watch

import (
	"context"
	"testing"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/progress"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/storage"
)

func TestHub(t *testing.T) {
	h := NewHub()
	h.begin("p", 1, time.Now())
	h.publish(LiveEvent{Run: 1, Kind: LiveRunStart})
	h.publish(LiveEvent{Run: 1, Kind: LiveStart, Provider: "p", Watch: 7, Title: "M"})
	n := float32(65)
	h.publish(LiveEvent{Run: 1, Kind: LiveChapter, Provider: "p", Watch: 7, Chapter: &n, Message: "downloading chapter 65"})
	if a := h.Active(); len(a) != 1 || a[0].Watch != 7 || a[0].Chapter == nil || *a[0].Chapter != 65 || a[0].State != "downloading chapter 65" {
		t.Errorf("active %+v", a)
	}

	// a subscriber mid-run gets the recent events first, then the new ones
	ctx, cancel := context.WithCancel(context.Background())
	ch := h.Subscribe(ctx)
	h.publish(LiveEvent{Run: 1, Kind: LiveDone, Provider: "p", Watch: 7})
	var seqs []uint64
	for range 4 {
		seqs = append(seqs, (<-ch).Seq)
	}
	if seqs[0] != 1 || seqs[3] != 4 {
		t.Errorf("seqs %v", seqs)
	}
	if a := h.Active(); a[0].Watch != 0 {
		t.Errorf("done watch still active %+v", a)
	}
	cancel()
	for range ch {
	}
	h.end("p")
	if len(h.Active()) != 0 {
		t.Error("ended provider active")
	}

	// a subscriber too far behind is dropped
	slow := h.Subscribe(context.Background())
	for range subscriberBuffer + 10 {
		h.publish(LiveEvent{Run: 2, Kind: LiveStart})
	}
	got := 0
	for range slow {
		got++
	}
	if got == 0 || got > subscriberBuffer+recentPerRun {
		t.Errorf("slow subscriber got %d", got)
	}

	// only the last finished runs are kept
	for run := int64(10); run < 10+keepFinished+2; run++ {
		h.publish(LiveEvent{Run: run, Kind: LiveRunDone})
	}
	runs := map[int64]bool{}
	for _, e := range h.Recent() {
		runs[e.Run] = true
	}
	if runs[10] || !runs[10+keepFinished+1] || len(h.Recent()) > 4+recentPerRun+keepFinished {
		t.Errorf("kept runs %v", runs)
	}
}

func TestLiveEventMessages(t *testing.T) {
	m := &fakeManga{info: mangadata.MangaInfo{ID: "m", Title: "M"}}
	w := storage.Watch{ID: 3, Provider: "p", Title: "M"}
	r := &client.Result{Chapter: m.chapter(65)}
	wait := Event{Kind: EventChapter, Watch: w, Chapter: &client.Event{Kind: client.EventWait, Result: r, Attempt: 2, Wait: 10 * time.Second}}
	if le, ok := liveEvent(9, wait); !ok || le.Kind != LiveWait || le.Run != 9 || le.Watch != 3 || le.Message != "chapter 65 rate limited (retry #2), waiting 10s" {
		t.Errorf("wait %+v", le)
	}
	found := Event{Kind: EventFound, Watch: w, Found: &Found{Chapters: 70, Picked: []float32{65.5, 66}, From: f32(65)}}
	if le, _ := liveEvent(9, found); le.Message != "found 70 chapters, 2 to download (65.5, 66)" {
		t.Errorf("found %q", le.Message)
	}
	nothing := Event{Kind: EventFound, Watch: w, Found: &Found{Chapters: 70, From: f32(65)}}
	if le, _ := liveEvent(9, nothing); le.Message != "found 70 chapters, nothing new from 65" {
		t.Errorf("nothing %q", le.Message)
	}
	pages := Event{Kind: EventChapter, Watch: w, Chapter: &client.Event{Kind: client.EventProgress, Result: r,
		Progress: &libmangal.DownloadProgress{Stage: libmangal.DownloadStageImages, Done: 23, Total: 57}}}
	if le, ok := liveEvent(9, pages); !ok || le.Kind != LiveProgress || le.Message != "chapter 65: 23/57 pages" || le.Pages != 57 || le.PagesDone != 23 {
		t.Errorf("progress %+v", le)
	}
	status := Event{Kind: EventStatus, Watch: w, Status: &progress.Status{Message: "MangaDex: rate limited"}}
	if le, ok := liveEvent(9, status); !ok || le.Kind != LiveStatus || le.Message != "MangaDex: rate limited" || le.Provider != "p" {
		t.Errorf("status %+v", le)
	}
	page := Event{Kind: EventStatus, Watch: w, Status: &progress.Status{Message: "MangaDex: 100 results so far, getting page 2", Minor: true}}
	if le, _ := liveEvent(9, page); le.Kind != LiveProgress {
		t.Errorf("minor status %+v", le)
	}

	// the page progress isn't kept for new subscribers
	h := NewHub()
	h.publish(LiveEvent{Run: 1, Kind: LiveProgress})
	h.publish(LiveEvent{Run: 1, Kind: LiveStatus})
	if recent := h.Recent(); len(recent) != 1 || recent[0].Kind != LiveStatus {
		t.Errorf("recent %+v", recent)
	}
}
