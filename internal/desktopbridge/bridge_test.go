package desktopbridge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

type fakeAgent struct {
	events    chan session.Event
	queued    chan session.Event
	stopped   atomic.Int32
	submitted atomic.Int32
	model     string
}

func (a *fakeAgent) Submit(context.Context, string) (<-chan session.Event, error) {
	a.submitted.Add(1)
	return a.events, nil
}
func (a *fakeAgent) Steer(string) (<-chan session.Event, error) { return make(chan session.Event), nil }
func (a *fakeAgent) FollowUp(string) (<-chan session.Event, error) {
	if a.queued != nil {
		return a.queued, nil
	}
	return make(chan session.Event), nil
}
func (a *fakeAgent) StopWork() error   { a.stopped.Add(1); return nil }
func (a *fakeAgent) Model() string     { return a.model }
func (a *fakeAgent) NeedsPerson() bool { return false }
func (a *fakeAgent) Title() string     { return "A conversation" }
func (a *fakeAgent) Transcript() []session.DisplayEntry {
	return []session.DisplayEntry{{Role: "user", Text: "hello"}}
}
func (a *fakeAgent) PlanTasks() []session.PlanTaskRow {
	return []session.PlanTaskRow{{ID: "child", Parent: "root", Status: "running", Waits: []string{"other"}}}
}
func (a *fakeAgent) ReadPlanTaskPage(string) (session.PlanTaskPage, bool, error) {
	return session.PlanTaskPage{}, true, nil
}
func (a *fakeAgent) Usage() session.Usage { return session.Usage{Calls: 1} }
func (a *fakeAgent) AttachReplay() ([]session.DisplayEntry, <-chan session.Event, func()) {
	return a.Transcript(), nil, func() {}
}

const testToken = "012345678901234567890123456789abcdef"

func fixture(t *testing.T) (*Bridge, *fakeAgent, string) {
	t.Helper()
	a := &fakeAgent{events: make(chan session.Event, 8), model: Model}
	b := New(testToken, func(file string) (Connection, error) {
		return Connection{Agent: a, Welcome: remote.Welcome{SessionFile: "session.jsonl", Workspace: "/project", Model: Model, Persistent: true, Launch: &remote.LaunchShape{OneModel: true}}, Close: func() {}}, nil
	})
	t.Cleanup(b.Close)
	w := request(b, "POST", "/api/engine/sessions", "{}")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var snapshot Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	return b, a, snapshot.ID
}
func request(b *Bridge, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	return w
}
func TestUnauthorizedCannotOpenOrSend(t *testing.T) {
	var opens atomic.Int32
	b := New(testToken, func(string) (Connection, error) { opens.Add(1); return Connection{}, nil })
	r := httptest.NewRequest("POST", "/api/engine/sessions", strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 401 || opens.Load() != 0 {
		t.Fatalf("status=%d opens=%d", w.Code, opens.Load())
	}
}
func TestSingleModelAndPersistentLaunchRequired(t *testing.T) {
	for _, kind := range []string{"model", "roles", "persistence"} {
		t.Run(kind, func(t *testing.T) {
			closed := false
			welcome := remote.Welcome{Model: Model, Persistent: true, Launch: &remote.LaunchShape{OneModel: true}}
			switch kind {
			case "model":
				welcome.Model = ""
			case "roles":
				welcome.Launch.OneModel = false
			case "persistence":
				welcome.Persistent = false
			}
			b := New(testToken, func(string) (Connection, error) {
				return Connection{Welcome: welcome, Close: func() { closed = true }}, nil
			})
			w := request(b, "POST", "/api/engine/sessions", "{}")
			if w.Code != 409 || !closed {
				t.Fatalf("status=%d closed=%v", w.Code, closed)
			}
		})
	}
}
func TestResumeDeduplicatesSameTranscriptAndKeepsCanonicalPlan(t *testing.T) {
	b, _, id := fixture(t)
	w := request(b, "POST", "/api/engine/sessions", `{"sessionFile":"session.jsonl"}`)
	var got Snapshot
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.ID != id || got.Tasks[0].Parent != "root" || got.Tasks[0].Waits[0] != "other" || got.Entries[0].Role != "user" {
		t.Fatalf("lost canonical identity or plan: %+v", got)
	}
}
func TestViewDisconnectDoesNotStopTurnAndReplaySurvives(t *testing.T) {
	b, a, id := fixture(t)
	path := "/api/engine/sessions/" + id
	w := request(b, "POST", path+"/turn", `{"text":"hello"}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("GET", path+"/events", nil).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+testToken)
	done := make(chan struct{})
	go func() { b.ServeHTTP(httptest.NewRecorder(), r); close(done) }()
	cancel()
	<-done
	a.events <- session.Event{Kind: session.EventTextDelta, Text: "reply", Addressed: true}
	a.events <- session.Event{Kind: session.EventTurnDone}
	close(a.events)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		b.sessions[id].mu.Lock()
		records := append([]Record(nil), b.sessions[id].records...)
		b.sessions[id].mu.Unlock()
		if len(records) == 3 {
			if records[0].Event.Text != "reply" || records[2].Snapshot.Running || a.stopped.Load() != 0 {
				t.Fatalf("view changed work: %+v", records)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("turn did not settle")
}
func TestAChosenConversationModelDoesNotBlockTheNextSend(t *testing.T) {
	b, a, id := fixture(t)
	a.model = "different"
	w := request(b, "POST", "/api/engine/sessions/"+id+"/turn", `{"text":"hello"}`)
	if w.Code != 200 {
		t.Fatalf("a chosen model refused a send: %d %s", w.Code, w.Body.String())
	}
}
func TestNativePreflightNeverCreatesSession(t *testing.T) {
	b, _, _ := fixture(t)
	r := httptest.NewRequest("OPTIONS", "/api/engine/sessions", nil)
	r.Header.Set("Origin", "tauri://localhost")
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "tauri://localhost" {
		t.Fatal("native preflight failed")
	}
}

func TestOtherSurfaceTurnUsesCanonicalFollowingStream(t *testing.T) {
	a := &fakeAgent{model: Model, events: make(chan session.Event)}
	follow := make(chan remote.Following, 1)
	b := New(testToken, func(string) (Connection, error) {
		return Connection{Agent: a, Welcome: remote.Welcome{Model: Model, Persistent: true, Launch: &remote.LaunchShape{OneModel: true}}, Follow: follow, Close: func() {}}, nil
	})
	w := request(b, "POST", "/api/engine/sessions", "{}")
	var snapshot Snapshot
	_ = json.Unmarshal(w.Body.Bytes(), &snapshot)
	events := make(chan session.Event, 2)
	events <- session.Event{Kind: session.EventTextDelta, Text: "from terminal", Addressed: true}
	events <- session.Event{Kind: session.EventTurnDone}
	close(events)
	follow <- remote.Following{Events: events}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s := b.sessions[snapshot.ID]
		s.mu.Lock()
		found := false
		for _, r := range s.records {
			if r.Event != nil && r.Event.Text == "from terminal" {
				found = true
			}
		}
		s.mu.Unlock()
		if found {
			if a.submitted.Load() != 0 {
				t.Fatal("watching another surface executed another turn")
			}
			b.Close()
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("terminal turn not delivered")
}

func TestQueuedFutureTurnIsRenderedAfterCurrentTurn(t *testing.T) {
	b, a, id := fixture(t)
	a.queued = make(chan session.Event, 2)
	path := "/api/engine/sessions/" + id
	if w := request(b, "POST", path+"/turn", `{"text":"first"}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(b, "POST", path+"/turn", `{"text":"next","mode":"queue"}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	close(a.events)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s := b.sessions[id]
		s.mu.Lock()
		running := s.running
		s.mu.Unlock()
		if !running {
			break
		}
		time.Sleep(time.Millisecond)
	}
	a.queued <- session.Event{Kind: session.EventTextDelta, Text: "queued response", Addressed: true}
	a.queued <- session.Event{Kind: session.EventTurnDone}
	close(a.queued)
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s := b.sessions[id]
		s.mu.Lock()
		found := false
		for _, r := range s.records {
			if r.Event != nil && r.Event.Text == "queued response" {
				found = true
			}
		}
		s.mu.Unlock()
		if found {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("queued output was discarded")
}

type stateAgent struct {
	*fakeAgent
	mu            sync.Mutex
	title         string
	rows          []session.PlanTaskRow
	readErr       error
	questions     []session.Question
	titles        chan session.Event
	questionsLane chan session.Event
	answered      session.Answer
	outcomes      []session.QuestionOutcome
	entries       []session.DisplayEntry
}

func (a *stateAgent) Transcript() []session.DisplayEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]session.DisplayEntry(nil), a.entries...)
}
func (a *stateAgent) Title() string { a.mu.Lock(); defer a.mu.Unlock(); return a.title }
func (a *stateAgent) ReadPlanTasks() ([]session.PlanTaskRow, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]session.PlanTaskRow(nil), a.rows...), a.readErr
}
func (a *stateAgent) WatchTitle() (<-chan session.Event, func()) { return a.titles, func() {} }
func (a *stateAgent) WatchQuestions() (<-chan session.Event, func()) {
	return a.questionsLane, func() {}
}
func (a *stateAgent) OpenQuestions() []session.Question {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]session.Question(nil), a.questions...)
}
func (a *stateAgent) RecentQuestionOutcomes(limit int) []session.QuestionOutcome {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]session.QuestionOutcome(nil), a.outcomes...)
}
func (a *stateAgent) ResolveQuestion(answer session.Answer) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if answer.Key != "1" {
		return errors.New("this answer was not offered")
	}
	a.answered = answer
	a.questions = nil
	return nil
}
func stateFixture(t *testing.T) (*Bridge, *stateAgent, string) {
	t.Helper()
	a := &stateAgent{fakeAgent: &fakeAgent{model: Model}, titles: make(chan session.Event, 2), questionsLane: make(chan session.Event, 2)}
	b := New(testToken, func(string) (Connection, error) {
		return Connection{Agent: a, Welcome: remote.Welcome{Model: Model, Persistent: true, Launch: &remote.LaunchShape{OneModel: true}}, Close: func() {}}, nil
	})
	t.Cleanup(b.Close)
	w := request(b, "POST", "/api/engine/sessions", "{}")
	var snapshot Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	return b, a, snapshot.ID
}
func TestCanonicalTitleArrivalAfterTurnPublishesSnapshot(t *testing.T) {
	b, a, id := stateFixture(t)
	a.mu.Lock()
	a.title = "the canonical generated name"
	a.mu.Unlock()
	a.titles <- session.Event{Kind: session.EventTitleChanged, Text: "the canonical generated name"}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s := b.sessions[id]
		s.mu.Lock()
		found := false
		for _, r := range s.records {
			if r.Snapshot != nil && r.Snapshot.Title == "the canonical generated name" {
				found = true
			}
		}
		stamp := s.updatedAt
		s.mu.Unlock()
		if found {
			if !stamp.IsZero() {
				t.Fatal("title replay fabricated last work activity")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("asynchronous name not published")
}
func TestPlanReadFailureRetainsCanonicalRowsAndReportsError(t *testing.T) {
	b, a, id := stateFixture(t)
	a.mu.Lock()
	a.rows = []session.PlanTaskRow{{ID: "real-task", Status: "running", Parent: "root"}}
	a.mu.Unlock()
	snapshot := b.sessions[id].snapshot()
	if len(snapshot.Tasks) != 1 {
		t.Fatal("missing initial rows")
	}
	a.mu.Lock()
	a.rows = nil
	a.readErr = errors.New("engine connection timed out")
	a.mu.Unlock()
	snapshot = b.sessions[id].snapshot()
	if snapshot.PlanError == "" || len(snapshot.Tasks) != 1 || snapshot.Tasks[0].ID != "real-task" {
		t.Fatalf("failed read presented empty plan: %+v", snapshot)
	}
}
func TestQuestionAnswerUsesCanonicalIdentityAndOfferedKey(t *testing.T) {
	b, a, id := stateFixture(t)
	a.mu.Lock()
	a.questions = []session.Question{{ID: 42, Kind: session.QuestionConsent, Head: "Run this command?", Ask: session.AskKind("permission"), Options: []session.AnswerOption{{Key: "1", Label: "Run once"}}}}
	a.mu.Unlock()
	path := "/api/engine/sessions/" + id + "/answer"
	if w := request(b, "POST", path, `{"kind":"consent","id":41,"key":"1"}`); w.Code != 409 {
		t.Fatal("stale identity answered")
	}
	if w := request(b, "POST", path, `{"kind":"consent","id":42,"key":"2"}`); w.Code != 409 {
		t.Fatal("unoffered key accepted")
	}
	if w := request(b, "POST", path, `{"kind":"consent","id":42,"key":"1","change":"keep existing data"}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	a.mu.Lock()
	answer := a.answered
	a.mu.Unlock()
	if answer.ID != 42 || answer.Key != "1" || answer.Change != "keep existing data" || answer.From != "desktop" || answer.At.IsZero() {
		t.Fatalf("answer lost canonical data: %+v", answer)
	}
}

func TestWorkerPlanChangePublishesWithoutTaskNoticeOrAICall(t *testing.T) {
	b, a, id := stateFixture(t)
	s := b.sessions[id]
	s.mu.Lock()
	s.observers = 1
	s.mu.Unlock()
	a.mu.Lock()
	a.rows = []session.PlanTaskRow{{ID: "child", Parent: "root", Status: "running"}}
	a.mu.Unlock()
	s.refreshPlanRead()
	s.mu.Lock()
	records := append([]Record(nil), s.records...)
	stamp := s.updatedAt
	s.mu.Unlock()
	if len(records) != 1 || records[0].Snapshot == nil || len(records[0].Snapshot.Tasks) != 1 || records[0].Snapshot.Tasks[0].ID != "child" {
		t.Fatal("worker-created row never reached view")
	}
	s.refreshPlanRead()
	s.mu.Lock()
	count := len(s.records)
	s.mu.Unlock()
	if count != 1 || !stamp.IsZero() || a.submitted.Load() != 0 {
		t.Fatal("read-only refresh fabricated activity or execution")
	}
}

func TestToolOutputFetchesOnlyNamedCanonicalCallStub(t *testing.T) {
	b, a, id := stateFixture(t)
	s := b.sessions[id]
	// macOS hands out temp folders under /var, a link to /private/var; the bridge
	// resolves links before it names the stub, so the expectation must too.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stubs := filepath.Join(dir, "logs", "stubs")
	if err := os.MkdirAll(stubs, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stubs, "0123456789abcdef.txt")
	if err := os.WriteFile(path, []byte("actual result"), 0600); err != nil {
		t.Fatal(err)
	}
	s.conn.Welcome.SessionFile = filepath.Join(dir, "transcript.jsonl")
	s.conn.Welcome.Workspace = dir
	var fetched atomic.Int32
	s.conn.FetchFile = func(got string) (remote.FetchedFile, error) {
		if got != path {
			t.Fatal("unexpected file selected")
		}
		fetched.Add(1)
		return remote.FetchedFile{Bytes: []byte("actual result")}, nil
	}
	a.mu.Lock()
	a.entries = []session.DisplayEntry{{Role: "tool", Tool: "bash", CallID: "known", Output: "[tool: bash · saved · full: " + path + "]"}, {Role: "tool", CallID: "unnamed", Output: "[tool: bash · saved · full: " + path + "]"}}
	a.mu.Unlock()
	for _, call := range []string{"missing", "unnamed"} {
		if w := request(b, "GET", "/api/engine/sessions/"+id+"/tools/"+call, ""); w.Code != 409 {
			t.Fatal("noncanonical call retrieved")
		}
	}
	w := request(b, "GET", "/api/engine/sessions/"+id+"/tools/known", "")
	var got ToolOutput
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || got.Output != "actual result" || !got.Full || fetched.Load() != 1 {
		t.Fatalf("canonical fullresult not retrieved: %s", w.Body.String())
	}
}
func TestToolOutputRejectsSymlinkOutsideSessionStubs(t *testing.T) {
	b, a, id := stateFixture(t)
	s := b.sessions[id]
	// macOS hands out temp folders under /var, a link to /private/var; the bridge
	// resolves links before it names the stub, so the expectation must too.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stubs := filepath.Join(dir, "logs", "stubs")
	_ = os.MkdirAll(stubs, 0700)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	_ = os.WriteFile(outside, []byte("unrelated file"), 0600)
	link := filepath.Join(stubs, "0123456789abcdef.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	s.conn.Welcome.SessionFile = filepath.Join(dir, "transcript.jsonl")
	s.conn.Welcome.Workspace = dir
	var fetched atomic.Int32
	s.conn.FetchFile = func(string) (remote.FetchedFile, error) { fetched.Add(1); return remote.FetchedFile{}, nil }
	a.mu.Lock()
	a.entries = []session.DisplayEntry{{Role: "tool", Tool: "bash", CallID: "known", Output: "[tool: bash · saved · full: " + link + "]"}}
	a.mu.Unlock()
	if _, err := s.toolOutput("known"); err == nil || fetched.Load() != 0 {
		t.Fatal("symlink escape crossed engine connection")
	}
}
func TestToolOutputDisplayLimitPreservesUTF8(t *testing.T) {
	b, a, id := stateFixture(t)
	a.mu.Lock()
	a.entries = []session.DisplayEntry{{Role: "tool", Tool: "bash", CallID: "known", Output: strings.Repeat("界", maxToolDisplayBytes)}}
	a.mu.Unlock()
	output, err := b.sessions[id].toolOutput("known")
	if err != nil || len(output.Output) > maxToolDisplayBytes || output.Full {
		t.Fatal("display cap orfullflagincorrect")
	}
	var roundtrip ToolOutput
	if err := json.Unmarshal([]byte(`{"output":"`+output.Output+`","full":false}`), &roundtrip); err != nil {
		t.Fatal("UTF8broken")
	}
}

func TestAnswerKeepsAskerOnlyWhereTheQuestionHasAPick(t *testing.T) {
	b, a, id := stateFixture(t)
	path := "/api/engine/sessions/" + id + "/answer"
	for _, tc := range []struct {
		name string
		pick *session.Pick
		want session.DecidedBy
	}{{"with pick", &session.Pick{Key: "1"}, session.DecidedByAsker}, {"without pick", nil, session.DecidedByPerson}} {
		a.mu.Lock()
		a.questions = []session.Question{{ID: 7, Kind: session.QuestionConsent, Head: "Keep going?", Options: []session.AnswerOption{{Key: "1", Label: "Yes"}}, Pick: tc.pick}}
		a.mu.Unlock()
		if w := request(b, "POST", path, `{"kind":"consent","id":7,"key":"1","decidedBy":"asker"}`); w.Code != 200 {
			t.Fatalf("%s: %s", tc.name, w.Body.String())
		}
		a.mu.Lock()
		got := a.answered.DecidedBy
		a.mu.Unlock()
		if got != tc.want {
			t.Fatalf("%s: decidedBy = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSnapshotCarriesRecentOutcomesOnlyWhenThereAreSome(t *testing.T) {
	b, a, id := stateFixture(t)
	path := "/api/engine/sessions/" + id
	if w := request(b, "GET", path, ""); strings.Contains(w.Body.String(), "recentOutcomes") {
		t.Fatalf("empty outcomes were not omitted: %s", w.Body.String())
	}
	at := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	a.mu.Lock()
	a.outcomes = []session.QuestionOutcome{{Kind: session.QuestionConsent, Token: "9", Head: "Run it?", Outcome: session.QuestionDecided, Words: "Allow once", By: "person", At: at, CallID: "c1"}}
	a.mu.Unlock()
	w := request(b, "GET", path, "")
	var snapshot Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	want := OutcomeWire{Kind: "consent", Token: "9", Head: "Run it?", Outcome: "decided", Words: "Allow once", By: "person", At: "2026-10-09T12:00:00Z", CallID: "c1"}
	if len(snapshot.RecentOutcomes) != 1 || snapshot.RecentOutcomes[0] != want {
		t.Fatalf("recent outcomes = %+v", snapshot.RecentOutcomes)
	}
	a.mu.Lock()
	a.outcomes[0].CallID = ""
	a.mu.Unlock()
	if w := request(b, "GET", path, ""); strings.Contains(w.Body.String(), "callId") {
		t.Fatalf("an outcome with no call must omit callId: %s", w.Body.String())
	}
}

// The snapshot preserves the actual wait and omits it for older outcomes.
func TestSnapshotCarriesClockReceiptElapsedSeconds(t *testing.T) {
	b, a, id := stateFixture(t)
	a.mu.Lock()
	a.outcomes = []session.QuestionOutcome{{Kind: session.QuestionAsk, Token: "4", Outcome: session.QuestionDecided, Words: "Keep strict", By: "dial", ElapsedSeconds: 37.2}}
	a.mu.Unlock()
	w := request(b, "GET", "/api/engine/sessions/"+id, "")
	var snapshot Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.RecentOutcomes) != 1 || snapshot.RecentOutcomes[0].ElapsedSeconds != 37.2 {
		t.Fatalf("clock receipt = %+v", snapshot.RecentOutcomes)
	}
}
