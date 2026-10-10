package desktopbridge

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestIncrementalSnapshotRouteAndOutputElision(t *testing.T) {
	b, a, id := stateFixture(t)
	a.entries = []session.DisplayEntry{{Role: "user", Text: "kept"}, {Role: "tool", CallID: "large", Output: strings.Repeat("x", outputCap+1)}}
	path := "/api/engine/sessions/" + id
	for _, since := range []string{"0", "1", "2", "3", "-1"} {
		w := request(b, "GET", path+"?since="+since, "")
		var tail SnapshotTail
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &tail) != nil {
			t.Fatalf("since %s: %s", since, w.Body.String())
		}
		wantFrom, wantReset := 0, false
		if since == "1" {
			wantFrom = 1
		}
		if since == "2" {
			wantFrom = 2
		}
		if since == "3" || since == "-1" {
			wantReset = true
		}
		if tail.From != wantFrom || tail.Reset != wantReset || tail.Header.EntryCount != 2 || len(tail.Entries) != 2-wantFrom {
			t.Fatalf("since %s: %+v", since, tail)
		}
		if len(tail.Entries) > 0 {
			e := tail.Entries[len(tail.Entries)-1]
			if !e.OutputOmitted || e.Output != "" || e.OutputBytes != outputCap+1 {
				t.Fatalf("output was not elided: %+v", e)
			}
		}
	}
	for _, since := range []string{"", "oops", "99999999999999999999999999"} {
		if w := request(b, "GET", path+"?since="+since, ""); w.Code != 400 {
			t.Fatalf("invalid since %q: %d", since, w.Code)
		}
	}
	var full elidedSnapshot
	w := request(b, "GET", path, "")
	if err := json.Unmarshal(w.Body.Bytes(), &full); err != nil {
		t.Fatal(err)
	}
	if full.EntryCount != 2 || len(full.Entries) != 2 || !full.Entries[1].OutputOmitted {
		t.Fatalf("full = %+v", full)
	}
	if len(a.entries[1].Output) != outputCap+1 {
		t.Fatal("canonical output was changed")
	}
}

func TestReplayRingRetainsHeadersAndOnlyNewEntries(t *testing.T) {
	b, a, id := stateFixture(t)
	s := b.sessions[id]
	a.entries = []session.DisplayEntry{{Role: "user", Text: "first"}}
	publish := func() { snapshot := s.snapshot(); s.publish(Record{Type: "snapshot", Snapshot: &snapshot}) }
	publish()
	a.entries = append(a.entries, session.DisplayEntry{Role: "tool", Output: strings.Repeat("x", outputCap+1)})
	publish()
	s.mu.Lock()
	first, second := s.records[0].Snapshot, s.records[1].Snapshot
	s.mu.Unlock()
	if first.From != 0 || len(first.Entries) != 1 || second.From != 1 || len(second.Entries) != 1 || !second.Entries[0].OutputOmitted {
		t.Fatalf("append replay = %+v, %+v", first, second)
	}
	for i := 0; i < MaxReplay; i++ {
		publish()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.records) != MaxReplay {
		t.Fatalf("ring size = %d", len(s.records))
	}
	for _, r := range s.records {
		if r.Snapshot == nil || r.Snapshot.Header.EntryCount != 2 || r.Snapshot.Header.Seq != r.Seq || r.Snapshot.From != 2 || len(r.Snapshot.Entries) != 0 {
			t.Fatalf("repeated header retained transcript: %+v", r)
		}
	}
}

func TestReplayRingResetsAfterTranscriptShrinks(t *testing.T) {
	b, a, id := stateFixture(t)
	s := b.sessions[id]
	for _, entries := range [][]session.DisplayEntry{
		{{Role: "user", Text: "first"}, {Role: "assistant", Text: "reply"}},
		{{Role: "user", Text: "summary"}},
	} {
		a.entries = entries
		snapshot := s.snapshot()
		s.publish(Record{Type: "snapshot", Snapshot: &snapshot})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tail := s.records[1].Snapshot
	if !tail.Reset || tail.From != 0 || tail.Header.EntryCount != 1 || len(tail.Entries) != 1 || tail.Entries[0].Text != "summary" {
		t.Fatalf("rewritten replay = %+v", tail)
	}
}

// Cancelling after the first data frame makes the gap test independent of clocks.
type cancelStream struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (w cancelStream) Write(p []byte) (int, error) {
	n, err := w.ResponseRecorder.Write(p)
	if strings.Contains(string(p), "data: ") {
		w.cancel()
	}
	return n, err
}

func TestReplayGapSendsOneFullElidedSnapshot(t *testing.T) {
	b, a, id := stateFixture(t)
	s := b.sessions[id]
	a.entries = []session.DisplayEntry{{Role: "user", Text: "kept"}, {Role: "tool", Output: strings.Repeat("x", outputCap+1)}}
	for i := 0; i < MaxReplay+1; i++ {
		s.publish(Record{Type: "event", Event: &Event{Kind: "text"}})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest("GET", "/api/engine/sessions/"+id+"/events?after=0", nil).WithContext(ctx)
	w := cancelStream{httptest.NewRecorder(), cancel}
	s.events(w, r)
	body := w.Body.String()
	if strings.Count(body, "data: ") != 1 {
		t.Fatalf("gap sent more than one snapshot: %.200s", body)
	}
	var wire struct {
		Seq      uint64
		Type     string
		Snapshot elidedSnapshot
	}
	line := strings.Split(strings.Split(body, "data: ")[1], "\n")[0]
	if err := json.Unmarshal([]byte(line), &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Type != "snapshot" || wire.Seq != MaxReplay+1 || wire.Snapshot.Seq != wire.Seq || len(wire.Snapshot.Entries) != 2 || !wire.Snapshot.Entries[1].OutputOmitted {
		t.Fatalf("gap = %+v", wire)
	}
}
