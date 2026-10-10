package desktopbridge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// flowAgent is a lifecycleAgent whose transcript is the saved conversation,
// so a reattach can be checked against that text and not the shared fixture line.
type flowAgent struct {
	lifecycleAgent
	text string
}

func (a *flowAgent) Transcript() []session.DisplayEntry {
	return []session.DisplayEntry{{Role: "user", Text: a.text}}
}

// flowChat is one saved conversation the opener can reopen by its session file.
type flowChat struct {
	file   string
	text   string
	events chan session.Event
	agent  *flowAgent
	closed atomic.Int32
	opens  atomic.Int32
	id     string
}

// TestLifecycleFlow is the renderer's reconnect contract for one bridge lifetime.
// Two saved conversations detach. A turn still running in the first keeps that
// engine past the idle limit, and only the quiet one is released. Its old id
// answers "reattach this conversation"; the next POST of its session file
// returns the same transcript. A restarted bridge knows none of those ids, and
// the same post is how the window gets each conversation back.
func TestLifecycleFlow(t *testing.T) {
	dir := t.TempDir()
	active := &flowChat{
		file:   filepath.Join(dir, "active", "session.jsonl"),
		text:   "still working on the first conversation",
		events: make(chan session.Event),
	}
	idle := &flowChat{
		file: filepath.Join(dir, "idle", "session.jsonl"),
		text: "the idle conversation's saved transcript",
	}
	byFile := map[string]*flowChat{active.file: active, idle.file: idle}
	open := func(file string) (Connection, error) {
		chat := byFile[file]
		if chat == nil {
			return Connection{}, errUnknownFlowSession
		}
		chat.opens.Add(1)
		chat.agent = &flowAgent{
			lifecycleAgent: lifecycleAgent{fakeAgent: fakeAgent{model: Model, events: chat.events}},
			text:           chat.text,
		}
		return Connection{
			Agent: chat.agent,
			Local: true,
			Welcome: remote.Welcome{
				SessionFile: file,
				Workspace:   filepath.Dir(file),
				Model:       Model,
				Persistent:  true,
				Launch:      &remote.LaunchShape{OneModel: true},
			},
			Close: func() { chat.closed.Add(1) },
		}, nil
	}

	clock := time.Now()
	bridge := New(testToken, open)
	t.Cleanup(func() {
		close(active.events)
		bridge.stopReaper()
		bridge.Close()
	})
	bridge.mu.Lock()
	bridge.lifecycleLocked().now = func() time.Time { return clock }
	bridge.mu.Unlock()

	active.id = openFlow(t, bridge, active.file)
	idle.id = openFlow(t, bridge, idle.file)
	if active.id == idle.id || active.opens.Load() != 1 || idle.opens.Load() != 1 {
		t.Fatalf("opens active=%s (%d) idle=%s (%d)", active.id, active.opens.Load(), idle.id, idle.opens.Load())
	}
	detachFlow(t, bridge, active.id)
	detachFlow(t, bridge, idle.id)

	turning := active.agent
	turn := request(bridge, "POST", "/api/engine/sessions/"+active.id+"/turn", `{"text":"keep going"}`)
	if turn.Code != http.StatusOK || !acceptedFlow(t, turn) || turning.submitted.Load() != 1 {
		t.Fatalf("turn: %d %s submitted=%d", turn.Code, turn.Body.String(), turning.submitted.Load())
	}

	// One sweep past the idle limit: the running turn is busy, the quiet chat is not.
	clock = clock.Add(2 * sessionIdleLimit)
	bridge.reapIdle(clock)
	bridge.reapIdle(clock.Add(sessionIdleLimit))

	if active.closed.Load() != 0 || turning.stopped.Load() != 0 {
		t.Fatalf("running conversation closed=%d stopped=%d", active.closed.Load(), turning.stopped.Load())
	}
	kept := readFlow(t, bridge, active.id)
	if !kept.Running || kept.SessionFile != active.file || transcriptFlow(kept) != active.text {
		t.Fatalf("running conversation changed: running=%v file=%s entries=%v", kept.Running, kept.SessionFile, kept.Entries)
	}
	if idle.closed.Load() != 1 || idle.agent.stopped.Load() != 0 {
		t.Fatalf("idle closed=%d stopped=%d", idle.closed.Load(), idle.agent.stopped.Load())
	}
	gone := request(bridge, "GET", "/api/engine/sessions/"+idle.id, "")
	if gone.Code != http.StatusNotFound || errorFlow(t, gone) != "reattach this conversation" {
		t.Fatalf("reaped id: %d %s", gone.Code, gone.Body.String())
	}
	staleTurn := request(bridge, "POST", "/api/engine/sessions/"+idle.id+"/turn", `{"text":"too late"}`)
	if staleTurn.Code != http.StatusNotFound || errorFlow(t, staleTurn) != "reattach this conversation" {
		t.Fatalf("stale turn: %d %s", staleTurn.Code, staleTurn.Body.String())
	}

	recovered := postFlow(t, bridge, idle.file)
	if recovered.ID == idle.id || recovered.SessionFile != idle.file || transcriptFlow(recovered) != idle.text || idle.opens.Load() != 2 {
		t.Fatalf("reattach lost the transcript: id=%s file=%s entries=%v opens=%d", recovered.ID, recovered.SessionFile, recovered.Entries, idle.opens.Load())
	}
	again := postFlow(t, bridge, idle.file)
	if again.ID != recovered.ID || transcriptFlow(again) != idle.text || idle.opens.Load() != 2 {
		t.Fatalf("second post opened another engine: id=%s opens=%d", again.ID, idle.opens.Load())
	}

	// A respawned bridge has an empty session table. The window still holds the
	// old ids; they answer the same sentence, and POST /sessions is the recovery.
	restarted := New(testToken, open)
	t.Cleanup(func() { restarted.stopReaper(); restarted.Close() })
	for _, id := range []string{active.id, idle.id, recovered.ID} {
		miss := request(restarted, "GET", "/api/engine/sessions/"+id, "")
		if miss.Code != http.StatusNotFound || errorFlow(t, miss) != "reattach this conversation" {
			t.Fatalf("restarted id %s: %d %s", id, miss.Code, miss.Body.String())
		}
	}
	backActive := postFlow(t, restarted, active.file)
	backIdle := postFlow(t, restarted, idle.file)
	if backActive.ID == active.id || transcriptFlow(backActive) != active.text || active.opens.Load() != 2 {
		t.Fatalf("restart lost the running conversation's transcript: %+v opens=%d", backActive, active.opens.Load())
	}
	if backIdle.ID == recovered.ID || transcriptFlow(backIdle) != idle.text || idle.opens.Load() != 3 {
		t.Fatalf("restart lost the idle transcript: %+v opens=%d", backIdle, idle.opens.Load())
	}
	if postFlow(t, restarted, active.file).ID != backActive.ID || active.opens.Load() != 2 {
		t.Fatalf("recovery post opened a second engine for %s", active.file)
	}
}

var errUnknownFlowSession = flowError("unknown session")

type flowError string

func (e flowError) Error() string { return string(e) }

func openFlow(t *testing.T, bridge *Bridge, file string) string {
	t.Helper()
	return postFlow(t, bridge, file).ID
}

func postFlow(t *testing.T, bridge *Bridge, file string) Snapshot {
	t.Helper()
	return decodeFlow(t, request(bridge, "POST", "/api/engine/sessions", `{"sessionFile":`+strconvQuote(file)+`}`))
}

func readFlow(t *testing.T, bridge *Bridge, id string) Snapshot {
	t.Helper()
	return decodeFlow(t, request(bridge, "GET", "/api/engine/sessions/"+id, ""))
}

func detachFlow(t *testing.T, bridge *Bridge, id string) {
	t.Helper()
	w := request(bridge, "POST", "/api/engine/sessions/"+id+"/detach", "")
	if w.Code != http.StatusOK || w.Body.String() != "{}\n" {
		t.Fatalf("detach %s: %d %s", id, w.Code, w.Body.String())
	}
}

func decodeFlow(t *testing.T, w *httptest.ResponseRecorder) Snapshot {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var snap Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	return snap
}

func errorFlow(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	return body.Error
}

func acceptedFlow(t *testing.T, w *httptest.ResponseRecorder) bool {
	t.Helper()
	var body struct {
		Accepted bool `json:"accepted"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Accepted
}

func transcriptFlow(snap Snapshot) string {
	if len(snap.Entries) != 1 {
		return ""
	}
	return snap.Entries[0].Text
}
