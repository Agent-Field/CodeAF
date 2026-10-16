package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// heldModel is a scripted model whose first turn is held open until the test
// lets it answer, so a correction and a queued message can arrive while the
// turn is still running. Every turn's words are kept.
type heldModel struct {
	server  *httptest.Server
	release chan struct{}
	arrived chan struct{}

	mu       sync.Mutex
	turns    int
	requests []recordedRequest
}

func newHeldModel(t *testing.T) *heldModel {
	t.Helper()
	model := &heldModel{release: make(chan struct{}), arrived: make(chan struct{})}
	model.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasSuffix(request.URL.Path, "/chat/completions") {
			http.Error(writer, "no catalog for a test", http.StatusServiceUnavailable)
			return
		}
		raw, _ := io.ReadAll(request.Body)
		var envelope recordedRequest
		if err := json.Unmarshal(raw, &envelope); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		held := false
		model.mu.Lock()
		model.requests = append(model.requests, envelope)
		if len(envelope.Tools) > 0 {
			model.turns++
			held = model.turns == 1
		}
		model.mu.Unlock()
		if held {
			close(model.arrived)
			select {
			case <-model.release:
			case <-request.Context().Done():
				return
			}
		}
		// A summary is asked for in prose and has to come back as prose.
		answer := "done"
		if strings.Contains(strings.Join(envelope.texts(), "\n"), "Write the summary now.") {
			answer = "The person opened a review with its background, then shared a set of specs and a set of links, and the agent acknowledged each message without deciding anything."
		}
		quoted, _ := json.Marshal(answer)
		if envelope.Stream {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = writer.Write([]byte(`data: {"id":"held","choices":[{"index":0,"delta":{"role":"assistant","content":` + string(quoted) + `},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"))
			return
		}
		_, _ = writer.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":` + string(quoted) + `},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(model.server.Close)
	return model
}

// appLiveSteer is the steer entry the backend said live for one client id
// once the turn read it.
func appLiveSteer(t *testing.T, rec *appRecorder, clientID string) appwire.Entry {
	t.Helper()
	i := rec.waitFor(t, 30*time.Second, func(event string, data any) bool {
		added, ok := data.(appwire.EntryAdded)
		return ok && added.Entry.Kind == appwire.EntrySteer && added.Entry.ClientID == clientID && added.Entry.State == appwire.SteerConsumed
	})
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.data[i].(appwire.EntryAdded).Entry
}

// sentWords is whether some turn's request carried words.
func (m *heldModel) sentWords(words string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, request := range m.requests {
		if len(request.Tools) > 0 && strings.Contains(strings.Join(request.texts(), "\n"), words) {
			return true
		}
	}
	return false
}

// A CORRECTION AND A QUEUED MESSAGE KEEP THEIR MARK ON THE HOSTED ROAD AND IN
// THE RECORD. A real conversation, hosted in this process over the engine's
// own wire, is held mid-turn; the person sends a marked correction and queues
// a marked message. The turn reads the correction at its next step and the
// queued message is sent when it ends. The model is sent the expanded words of
// both; the steer entry and the queued message's user entry show the person's
// words and reads live; the record keeps each as a message of theirs; a
// compaction that summarizes them leaves them; and a NEW backend reading the record off the disk
// shows exactly the same on the same lines.
func TestAppSteerAndQueueReadsSurviveTheHostedRoadAndAReopen(t *testing.T) {
	root := resolvedTempDir(t)
	_, workspace := wakeEnvironment(t, root)
	model := newHeldModel(t)
	t.Setenv("CODEAF_BASE_URL", model.server.URL)
	rec := newAppRecorder()
	first := newAppBackend(rec, appOpenInProcess)
	closed := false
	defer func() {
		if !closed {
			first.Close()
		}
	}()
	agent, err := mustCreate(t, first, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	// The first message is long enough that a compaction has something to
	// summarize once the marked messages are behind the last three.
	opening := "start the review\n\n" + strings.TrimSpace(strings.Repeat("background on the review, one line of it\n", 160))
	if _, err := first.Send(agent.ID, opening, "h-0"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-model.arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("the first turn never reached the model")
	}

	steerSaid, steerBody := "use the specs", "use the specs\n\nSpecs:\n- docs/a.md\n- https://example.com/b"
	if err := first.SteerMessage(agent.ID, appwire.AgentMessage{Text: steerBody, Said: &steerSaid, ClientID: "h-1", Reads: []appwire.MessageRead{appSpecsRead()}}); err != nil {
		t.Fatal(err)
	}
	none, queueBody := "", "Links:\n- https://example.com/c"
	queued, err := first.QueueMessage(agent.ID, appwire.AgentMessage{Text: queueBody, Said: &none, ClientID: "h-2", Reads: []appwire.MessageRead{appCutRead()}})
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) != 1 || queued[0].Text != "" || !reflect.DeepEqual(queued[0].Reads, []appwire.MessageRead{appCutRead()}) {
		t.Fatalf("the queue shows %+v", queued)
	}
	close(model.release)

	steered := appLiveSteer(t, rec, "h-1")
	if steered.Text != steerSaid || !reflect.DeepEqual(steered.Reads, []appwire.MessageRead{appSpecsRead()}) {
		t.Fatalf("the consumed correction went live as %+v", steered)
	}
	sent := appLiveUser(t, rec, "h-2")
	if sent.Text != "" || !reflect.DeepEqual(sent.Reads, []appwire.MessageRead{appCutRead()}) {
		t.Fatalf("the queued message went live as %+v", sent)
	}
	appTurnDoneFor(t, rec, sent.Turn)
	if !model.sentWords(steerBody) || !model.sentWords(queueBody) {
		t.Fatal("the model was not sent the expanded words of both")
	}

	// A compaction summarizes what is before the last three messages, so three
	// plain ones put the marked ones inside what it summarizes.
	for _, words := range []string{"one", "two", "three"} {
		appTurnDoneFor(t, rec, appSendMessageWhenIdle(t, first, agent.ID, appwire.AgentMessage{Text: words, ClientID: "p-" + words}))
	}
	want := func(t *testing.T, users []appwire.Entry, when string) {
		t.Helper()
		if len(users) != 6 || users[5].Text != "three" || users[5].Reads != nil || users[0].Text != opening || users[0].Reads != nil ||
			users[1].Text != steerSaid || !reflect.DeepEqual(users[1].Reads, []appwire.MessageRead{appSpecsRead()}) ||
			users[2].Text != "" || !reflect.DeepEqual(users[2].Reads, []appwire.MessageRead{appCutRead()}) {
			t.Fatalf("%s the person's messages are %+v", when, users)
		}
	}
	entries, _, err := first.Entries(agent.ID, "", appwire.MaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	want(t, appUsersOf(entries), "before a compaction")

	if err := first.CompactAgent(agent.ID); err != nil {
		t.Fatal(err)
	}
	ended := rec.waitFor(t, 30*time.Second, func(event string, data any) bool {
		_, ok := data.(appwire.CompactionEnded)
		return ok
	})
	rec.mu.Lock()
	outcome := rec.data[ended].(appwire.CompactionEnded)
	rec.mu.Unlock()
	if outcome.Outcome != appwire.CompactionCompacted {
		t.Fatalf("the compaction ended %+v", outcome)
	}
	if entries, _, err = first.Entries(agent.ID, "", appwire.MaxEntries); err != nil {
		t.Fatal(err)
	}
	want(t, appUsersOf(entries), "after a compaction")
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true

	second := newAppBackend(newAppRecorder(), appOpenInProcess)
	defer second.Close()
	if entries, _, err = second.Entries(agent.ID, "", appwire.MaxEntries); err != nil {
		t.Fatal(err)
	}
	want(t, appUsersOf(entries), "after a reopen")
}
