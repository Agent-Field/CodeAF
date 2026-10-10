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
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

type lifecycleAgent struct {
	fakeAgent
	questions []session.Question
	rows      []session.PlanTaskRow
	readErr   error
}

func (a *lifecycleAgent) OpenQuestions() []session.Question             { return a.questions }
func (a *lifecycleAgent) PlanTasks() []session.PlanTaskRow              { return a.rows }
func (a *lifecycleAgent) ReadPlanTasks() ([]session.PlanTaskRow, error) { return a.rows, a.readErr }

type lifecycleFixture struct {
	b        *Bridge
	a        *lifecycleAgent
	id, file string
	now      time.Time
	closed   atomic.Int32
	opens    atomic.Int32
}

func newLifecycleFixture(t *testing.T) *lifecycleFixture {
	t.Helper()
	f := &lifecycleFixture{now: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC), file: filepath.Join(t.TempDir(), "session.jsonl")}
	f.a = &lifecycleAgent{fakeAgent: fakeAgent{model: Model}}
	f.b = New(testToken, func(file string) (Connection, error) {
		f.opens.Add(1)
		return Connection{Agent: f.a, Local: true,
			Welcome: remote.Welcome{SessionFile: f.file, Workspace: t.TempDir(), Model: Model, Persistent: true, Launch: &remote.LaunchShape{OneModel: true}},
			Close:   func() { f.closed.Add(1) }}, nil
	})
	t.Cleanup(func() { f.b.stopReaper(); f.b.Close() })
	f.b.mu.Lock()
	f.b.lifecycleLocked().now = func() time.Time { return f.now }
	f.b.mu.Unlock()
	f.id = f.attach(t)
	return f
}

func (f *lifecycleFixture) attach(t *testing.T) string {
	t.Helper()
	w := request(f.b, "POST", "/api/engine/sessions", `{"sessionFile":`+strconvQuote(f.file)+`}`)
	if w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	var snapshot Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	// The integrator owns the POST route. Exercise its attachment hook here
	// until both route branches call it directly.
	f.b.mu.Lock()
	f.b.attachViewLocked(f.b.sessions[snapshot.ID])
	f.b.mu.Unlock()
	return snapshot.ID
}

func strconvQuote(s string) string { raw, _ := json.Marshal(s); return string(raw) }

func (f *lifecycleFixture) detach(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/engine/sessions/"+f.id+"/detach", nil)
	// The handler is complete before the integrator adds the detach case to
	// extra. Other HTTP behavior is exercised through the exported ServeHTTP.
	http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.b.detach(w, r, f.b.sessions[f.id]) }).ServeHTTP(w, r)
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "{}" {
		t.Fatalf("detach: %d %s", w.Code, w.Body.String())
	}
	return w
}

func (f *lifecycleFixture) sweep(after time.Duration) {
	f.now = f.now.Add(after)
	f.b.reapIdle(f.now)
}

func (f *lifecycleFixture) assertPresent(t *testing.T) {
	t.Helper()
	if w := request(f.b, "GET", "/api/engine/sessions/"+f.id, ""); w.Code != http.StatusOK {
		t.Fatalf("engine disappeared: %d %s", w.Code, w.Body.String())
	}
	if f.closed.Load() != 0 || f.a.stopped.Load() != 0 {
		t.Fatalf("closed=%d stopped=%d", f.closed.Load(), f.a.stopped.Load())
	}
}

func TestDetachThenIdleReapsTheEngine(t *testing.T) {
	f := newLifecycleFixture(t)
	s := f.b.sessions[f.id]
	f.detach(t)
	f.sweep(sessionIdleLimit - time.Nanosecond)
	f.assertPresent(t)
	f.sweep(time.Nanosecond)
	w := request(f.b, "GET", "/api/engine/sessions/"+f.id, "")
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "reattach this conversation") {
		t.Fatalf("reaped id: %d %s", w.Code, w.Body.String())
	}
	if f.closed.Load() != 1 || f.a.stopped.Load() != 0 {
		t.Fatalf("closed=%d stopped=%d", f.closed.Load(), f.a.stopped.Load())
	}
	select {
	case <-s.done:
	default:
		t.Fatal("conversation watchers were not stopped")
	}
	f.b.Close()
	if f.closed.Load() != 1 {
		t.Fatal("shutdown closed a reaped engine twice")
	}
}

func TestRunningWorkIsNeverReaped(t *testing.T) {
	for _, work := range []string{"turn", "plan", "claimed", "descendants", "unknown"} {
		t.Run(work, func(t *testing.T) {
			f := newLifecycleFixture(t)
			s := f.b.sessions[f.id]
			switch work {
			case "turn":
				s.mu.Lock()
				s.running = true
				s.mu.Unlock()
			case "plan":
				f.a.rows = []session.PlanTaskRow{{Status: "running"}}
			case "claimed":
				f.a.rows = []session.PlanTaskRow{{Status: "claimed"}}
			case "descendants":
				f.a.rows = []session.PlanTaskRow{{Running: 1}}
			case "unknown":
				f.a.readErr = errors.New("engine unavailable")
			}
			f.detach(t)
			f.sweep(2 * sessionIdleLimit)
			f.assertPresent(t)
		})
	}
}

func TestAnOpenQuestionIsNeverReaped(t *testing.T) {
	f := newLifecycleFixture(t)
	f.a.questions = []session.Question{{ID: 1}}
	f.detach(t)
	f.sweep(2 * sessionIdleLimit)
	f.assertPresent(t)
	f.a.questions = nil
	f.sweep(0)
	f.sweep(sessionIdleLimit - time.Nanosecond)
	f.assertPresent(t)
	f.sweep(time.Nanosecond)
	if f.closed.Load() != 1 {
		t.Fatal("answered question prevented eventual reaping")
	}
}

func TestALiveTerminalIsNeverReaped(t *testing.T) {
	for _, command := range []string{"", "build"} {
		t.Run("command="+command, func(t *testing.T) {
			f := newLifecycleFixture(t)
			s := f.b.sessions[f.id]
			term := &terminal{id: "term", command: command, started: f.now}
			set := s.terminals()
			set.mu.Lock()
			set.add(term)
			set.mu.Unlock()
			// This is a process-state fixture, so cleanup removes it without
			// sending a signal to a process the test never started.
			t.Cleanup(func() { set.remove(term.id) })
			f.detach(t)
			f.sweep(2 * sessionIdleLimit)
			f.assertPresent(t)
		})
	}
}

func TestReapedConversationReattachesBySessionFile(t *testing.T) {
	f := newLifecycleFixture(t)
	f.detach(t)
	f.sweep(sessionIdleLimit)
	id := f.attach(t)
	if id == f.id || f.opens.Load() != 2 {
		t.Fatalf("id=%s opens=%d", id, f.opens.Load())
	}
	w := request(f.b, "GET", "/api/engine/sessions/"+id, "")
	var got Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SessionFile != f.file || len(got.Entries) != 1 || got.Entries[0].Text != "hello" {
		t.Fatalf("canonical conversation lost: %+v", got)
	}
}

func TestLifecycleViewsAndObserversPreventReaping(t *testing.T) {
	f := newLifecycleFixture(t)
	if id := f.attach(t); id != f.id {
		t.Fatal("second view opened a second engine")
	}
	f.detach(t)
	f.sweep(2 * sessionIdleLimit)
	f.assertPresent(t)
	f.detach(t)
	s := f.b.sessions[f.id]
	s.mu.Lock()
	s.observers++
	s.mu.Unlock()
	f.sweep(2 * sessionIdleLimit)
	f.assertPresent(t)
	s.mu.Lock()
	s.observers--
	s.mu.Unlock()
	f.sweep(0)
	f.sweep(sessionIdleLimit)
	if f.closed.Load() != 1 {
		t.Fatal("last observer left but engine survived")
	}
}

func TestLifecycleRepeatedDetachDoesNotExtendIdle(t *testing.T) {
	f := newLifecycleFixture(t)
	f.detach(t)
	f.sweep(sessionIdleLimit / 2)
	f.detach(t)
	f.sweep(sessionIdleLimit / 2)
	if f.closed.Load() != 1 {
		t.Fatal("repeated detach postponed reaping")
	}
}

func TestLifecycleLiveSSEObserverPreventsReaping(t *testing.T) {
	f := newLifecycleFixture(t)
	ended := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(ended)
		f.b.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/engine/sessions/"+f.id+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+testToken)
	response, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("SSE status=%d", response.StatusCode)
	}
	f.detach(t)
	f.sweep(2 * sessionIdleLimit)
	f.assertPresent(t)
	response.Body.Close()
	<-ended
	f.sweep(0)
	f.sweep(sessionIdleLimit)
	if f.closed.Load() != 1 {
		t.Fatal("engine survived after real SSE disconnected and grace expired")
	}
}

func TestLifecycleActivityRestartsIdle(t *testing.T) {
	f := newLifecycleFixture(t)
	f.detach(t)
	f.sweep(sessionIdleLimit / 2)
	f.b.sessions[f.id].publish(Record{Type: "event"})
	f.sweep(sessionIdleLimit / 2)
	f.assertPresent(t)
	f.sweep(sessionIdleLimit)
	if f.closed.Load() != 1 {
		t.Fatal("quiet engine was not reaped")
	}
}

func TestLifecycleCanonicalJobPreventsReaping(t *testing.T) {
	f := newLifecycleFixture(t)
	p := session.SessionPresence{Schema: 1, SessionID: "chat", UpdatedAt: f.now.Add(2 * sessionIdleLimit), Jobs: []session.PresenceJob{{}}}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(f.file), "presence.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	f.detach(t)
	f.sweep(2 * sessionIdleLimit)
	f.assertPresent(t)
}

func TestLifecycleDetachRejectsGetAndStaleConversation(t *testing.T) {
	f := newLifecycleFixture(t)
	s := f.b.sessions[f.id]
	w := httptest.NewRecorder()
	f.b.detach(w, httptest.NewRequest("GET", "/detach", nil), s)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status=%d", w.Code)
	}
	f.detach(t)
	f.sweep(sessionIdleLimit)
	w = httptest.NewRecorder()
	f.b.detach(w, httptest.NewRequest("POST", "/detach", nil), s)
	if w.Code != http.StatusNotFound {
		t.Fatalf("stale detach status=%d", w.Code)
	}
}

func TestLifecycleReaperUsesInjectedTicksAndStops(t *testing.T) {
	// New starts the production reaper, and startReaper ignores a second start.
	// This clock has to be the first one, so the bridge is built without New.
	f := &lifecycleFixture{now: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC), file: filepath.Join(t.TempDir(), "session.jsonl")}
	f.a = &lifecycleAgent{fakeAgent: fakeAgent{model: Model}}
	f.b = &Bridge{
		token: testToken, sessions: map[string]*conversation{}, icons: newFaviconCache(),
		open: func(string) (Connection, error) {
			f.opens.Add(1)
			return Connection{Agent: f.a, Local: true,
				Welcome: remote.Welcome{SessionFile: f.file, Workspace: t.TempDir(), Model: Model, Persistent: true, Launch: &remote.LaunchShape{OneModel: true}},
				Close:   func() { f.closed.Add(1) }}, nil
		},
	}
	t.Cleanup(func() { f.b.stopReaper(); f.b.Close() })
	f.b.mu.Lock()
	f.b.lifecycleLocked().now = func() time.Time { return f.now }
	f.b.mu.Unlock()
	f.id = f.attach(t)
	f.detach(t)
	ticks := make(chan time.Time)
	now := f.now.Add(sessionIdleLimit)
	f.b.startReaper(func() time.Time { return now }, ticks)
	f.b.startReaper(func() time.Time { return now }, ticks)
	ticks <- now
	// A second received tick proves the preceding sweep completed, without
	// a wall-clock sleep or unsynchronized reads of the session map.
	ticks <- now
	f.b.stopReaper()
	f.b.stopReaper()
	if f.closed.Load() != 1 {
		t.Fatalf("fake tick closed %d engines", f.closed.Load())
	}
}
