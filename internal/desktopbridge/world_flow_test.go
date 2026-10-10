package desktopbridge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The world flow, end to end through the real routes: two windows watch
// GET /events, an attached chat asks, an unattached chat needs the person, and
// the Inbox answers either one through POST /sessions {sessionFile} then
// /answer. Only the engine behind the connection is a stub.

// flowRig is a bridge whose rows and attention producers publish onto its feed,
// exactly as a door that wires them would.
type flowRig struct {
	t      *testing.T
	srv    *httptest.Server
	feed   *WorldFeed
	rows   *rowsFixture
	agents map[string]Agent
}

func newFlowRig(t *testing.T, agents map[string]Agent) *flowRig {
	t.Helper()
	feed, _ := testFeed(&fakeWorld{})
	rig := &flowRig{t: t, feed: feed, rows: newRowsFixture(t), agents: agents}
	rows := newWorldRows(rig.rows.root, feed.record)
	rows.newTicker = rig.rows.clock.ticker
	newAttention(rows, feed.record)
	rig.rows.rows = rows

	b := New(worldToken, func(file string) (Connection, error) {
		agent, ok := rig.agents[file]
		if !ok {
			t.Errorf("open of unexpected file %q", file)
		}
		return Connection{
			Agent: agent,
			Welcome: remote.Welcome{
				SessionFile: file, Workspace: "/work/repo", Model: Model, Persistent: true,
				Launch: &remote.LaunchShape{OneModel: true},
			},
			Close: func() {},
		}, nil
	})
	b.UseWorld(feed)
	b.UseWorldRows(rows)
	rig.srv = httptest.NewServer(b.Handler())
	t.Cleanup(func() { rig.srv.CloseClientConnections(); rig.srv.Close(); b.Close() })
	return rig
}

func (r *flowRig) post(path, body string) Snapshot {
	r.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, r.srv.URL+"/api/engine"+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+worldToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	var snap Snapshot
	_ = json.NewDecoder(resp.Body).Decode(&snap)
	if resp.StatusCode != 200 {
		r.t.Fatalf("POST %s: %d", path, resp.StatusCode)
	}
	return snap
}

// attention reads the stream until the next attention record and returns it with
// the raw record, so two observers can be compared byte for byte.
func (s *sse) attention(t *testing.T) (WorldRecord, inboxDelta) {
	t.Helper()
	for {
		rec, _ := s.next(t)
		if rec.Type != attentionKind {
			continue
		}
		var delta inboxDelta
		if err := json.Unmarshal(rec.Payload, &delta); err != nil {
			t.Fatal(err)
		}
		return rec, delta
	}
}

func itemIDs(items []InboxItem) string {
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	return strings.Join(ids, ",")
}

func newFlowChatAgent(question session.Question) *stateAgent {
	return &stateAgent{
		fakeAgent:     &fakeAgent{events: make(chan session.Event, 8), model: Model},
		titles:        make(chan session.Event, 2),
		questionsLane: make(chan session.Event, 2),
		questions:     []session.Question{question},
	}
}

func TestWorldFlowAttentionAndLazyAnswer(t *testing.T) {
	attachedQ := session.Question{ID: 7, Kind: "task", Ask: session.AskChoice, Head: "Which parser?"}
	lazyQ := session.Question{ID: 9, Kind: "task", Ask: session.AskChoice, Head: "Ship it?"}
	attachedAgent := newFlowChatAgent(attachedQ)
	attachedAgent.title = "Lexer"
	lazyAgent := newFlowChatAgent(lazyQ)
	lazyAgent.title = "Parser"

	rig := newFlowRig(t, nil)
	fileA := rig.rows.chat("aaaa", "Lexer", "")
	fileB := rig.rows.chat("bbbb", "Parser", string(session.PresenceWaiting))
	rig.agents = map[string]Agent{fileA: attachedAgent, fileB: lazyAgent}

	one := openStream(t, rig.srv, "0")
	two := openStream(t, rig.srv, "0")
	for _, s := range []*sse{one, two} {
		if rec, _ := s.next(t); rec.Type != "reset" {
			t.Fatalf("first record = %q, want reset", rec.Type)
		}
	}

	// An unattached chat that needs the person appears with its title and nothing
	// else: no head, no session id, because no window holds it.
	rig.rows.rows.Scan()
	for name, s := range map[string]*sse{"one": one, "two": two} {
		_, delta := s.attention(t)
		if len(delta.Items) != 1 || delta.Items[0].ID != "bbbb:question" || delta.Items[0].Head != "" || delta.Items[0].SessionID != "" || delta.Items[0].Title != "Parser" {
			t.Fatalf("observer %s, unattached: %+v", name, delta.Items)
		}
	}

	// An attached chat asks: ONE attention record, the same on both observers.
	snapA := rig.post("/sessions", `{"sessionFile":`+flowQuote(fileA)+`}`)
	attachedAgent.questionsLane <- session.Event{}
	recOne, deltaOne := one.attention(t)
	recTwo, deltaTwo := two.attention(t)
	if recOne.Seq != recTwo.Seq || string(recOne.Payload) != string(recTwo.Payload) {
		t.Fatalf("observers disagree: %d %s vs %d %s", recOne.Seq, recOne.Payload, recTwo.Seq, recTwo.Payload)
	}
	if itemIDs(deltaOne.Items) != "aaaa:7,bbbb:question" || deltaTwo.Items[0].Head != "Which parser?" || deltaTwo.Items[0].SessionID != "aaaa" {
		t.Fatalf("attached question: %+v", deltaOne.Items)
	}
	one.quiet(t)
	two.quiet(t)

	// Answering the unattached chat attaches it lazily (the same file never opens
	// twice) and then answers by the question's own token.
	snapB := rig.post("/sessions", `{"sessionFile":`+flowQuote(fileB)+`}`)
	if snapB.ID == "" || snapB.ID == snapA.ID {
		t.Fatalf("lazy attach gave session %q (first %q)", snapB.ID, snapA.ID)
	}
	if again := rig.post("/sessions", `{"sessionFile":`+flowQuote(fileB)+`}`); again.ID != snapB.ID {
		t.Fatalf("a second attach of one file opened %q, want %q", again.ID, snapB.ID)
	}
	rig.post("/sessions/"+snapB.ID+"/answer", `{"kind":"task","id":9,"key":"1"}`)
	for name, s := range map[string]*sse{"one": one, "two": two} {
		var last []InboxItem
		for i := 0; i < 3 && itemIDs(last) != "aaaa:7"; i++ {
			_, delta := s.attention(t)
			last = delta.Items
		}
		if itemIDs(last) != "aaaa:7" {
			t.Fatalf("observer %s after the answer: %q, want only aaaa:7", name, itemIDs(last))
		}
	}
	if lazyAgent.answered.ID != 9 || lazyAgent.answered.From != "desktop" {
		t.Fatalf("the engine heard %+v", lazyAgent.answered)
	}
}

func TestWorldFlowJobUpdateRecordsOnTheFeed(t *testing.T) {
	agent := &jobsAgent{fakeAgent: &fakeAgent{events: make(chan session.Event, 8), model: Model}}
	notice := session.JobNotice{ID: 4, Name: "bench", Command: "make bench", Kind: session.JobKindWatch, State: session.JobRunning, Started: time.Now(), LogPath: "/places/cccc/jobs/4.log"}
	agent.rows = []session.JobNotice{notice}
	rig := newFlowRig(t, nil)
	file := "/places/cccc/transcript.jsonl"
	rig.agents = map[string]Agent{file: agent}

	watcher := openStream(t, rig.srv, "0")
	if rec, _ := watcher.next(t); rec.Type != "reset" {
		t.Fatalf("first record = %q, want reset", rec.Type)
	}
	snap := rig.post("/sessions", `{"sessionFile":`+flowQuote(file)+`}`)
	rig.post("/sessions/"+snap.ID+"/turn", `{"text":"hello"}`)
	agent.events <- session.Event{Kind: session.EventJobUpdate, Job: &notice}
	for {
		rec, _ := watcher.next(t)
		if rec.Type != jobsWorldKind {
			continue
		}
		var got jobsRollup
		if err := json.Unmarshal(rec.Payload, &got); err != nil {
			t.Fatal(err)
		}
		if got.ChatID != "cccc" || got.Running != 1 || len(got.Jobs) != 1 || got.Jobs[0].ID != 4 {
			t.Fatalf("jobs record: %+v", got)
		}
		return
	}
}

func TestWorldFlowResumingPastTheRingGetsOneReset(t *testing.T) {
	fw := &fakeWorld{}
	feed, _ := testFeed(fw)
	feed.ringSize = 4
	srv := bridgeFor(t, feed)
	for i := 0; i < 10; i++ {
		feed.record(jobsWorldKind, jobsRollup{ChatID: "cccc", Running: i})
	}

	late := openStream(t, srv, "2")
	rec, _ := late.next(t)
	if rec.Type != "reset" || rec.Seq != 10 {
		t.Fatalf("late reader got %q at %d, want one reset at 10", rec.Type, rec.Seq)
	}
	late.quiet(t)

	// A reader still inside the ring is replayed, never reset.
	inside := openStream(t, srv, "8")
	for _, want := range []uint64{9, 10} {
		if rec, _ := inside.next(t); rec.Type != jobsWorldKind || rec.Seq != want {
			t.Fatalf("replay got %q at %d, want jobs at %d", rec.Type, rec.Seq, want)
		}
	}
}

func flowQuote(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}
