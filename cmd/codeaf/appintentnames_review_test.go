package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A real in-process engine and wire client, with the local HTTP title response
// withheld until AFTER turn.ended. A turn-only title listener cannot pass this.
func TestAppIntentTitleAfterTurnEndsReachesNativeProjection(t *testing.T) {
	root := resolvedTempDir(t)
	model, workspace := wakeEnvironment(t, root)
	arrived, release := make(chan struct{}), make(chan struct{})
	var started, unblock sync.Once
	defer unblock.Do(func() { close(release) })
	original := model.server.Config.Handler
	model.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var request recordedRequest
		_ = json.Unmarshal(raw, &request)
		words := request.texts()
		if len(words) > 0 && words[0] == "You name conversations." {
			started.Do(func() { close(arrived) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		original.ServeHTTP(w, r)
	})
	rec := newAppRecorder()
	backend := newAppBackend(rec, appOpenInProcess)
	defer backend.Close()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace, Text: "repair the tokenizer"})
	if err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 10*time.Second, appTurnDone)
	select {
	case <-arrived:
	case <-time.After(time.Second):
		t.Fatal("title request never arrived")
	}
	unblock.Do(func() { close(release) })
	rec.waitFor(t, 2*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && event == appwire.EventAgentChanged && c.Agent.ID == agent.ID && c.Agent.Title == "done"
	})
	if _, err := backend.Rename(agent.ID, "My tokenizer work"); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Rename(agent.ID, "My final tokenizer name"); err != nil {
		t.Fatal(err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	fresh := newAppBackend(newAppRecorder(), appOpenInProcess)
	defer fresh.Close()
	agents, err := fresh.ListAgents(workspace)
	if err != nil || len(agents) != 1 || agents[0].Title != "My final tokenizer name" {
		t.Fatalf("reopened agents=%+v error=%v", agents, err)
	}
	if got := len(namerRequests(model)); got != 1 {
		t.Fatalf("title requests=%d", got)
	}
}

type intentTitleEmitter func(string, any)

func (f intentTitleEmitter) Emit(event string, data any) { f(event, data) }

// Pause actual Outbox delivery of the generated view, then rename through the
// real app RPC. A rename emitted outside that same queue can be overtaken by
// the paused generated view, even if the stored current name is already manual.
func TestAppManualRenameStaysLastWhenGeneratedBoxDeliveryIsPaused(t *testing.T) {
	root := resolvedTempDir(t)
	model, workspace := wakeEnvironment(t, root)
	titleGate := make(chan struct{})
	var releaseTitle sync.Once
	defer releaseTitle.Do(func() { close(titleGate) })
	original := model.server.Config.Handler
	model.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var request recordedRequest
		_ = json.Unmarshal(raw, &request)
		words := request.texts()
		if len(words) > 0 && words[0] == "You name conversations." {
			select {
			case <-titleGate:
			case <-r.Context().Done():
				return
			}
		}
		original.ServeHTTP(w, r)
	})
	rec := newAppRecorder()
	paused, resume := make(chan struct{}), make(chan struct{})
	var pauseOnce, resumeOnce sync.Once
	defer resumeOnce.Do(func() { close(resume) })
	emit := intentTitleEmitter(func(event string, data any) {
		if change, ok := data.(appwire.AgentChanged); ok && change.Agent.Title == "done" {
			pauseOnce.Do(func() { close(paused); <-resume })
		}
		rec.Emit(event, data)
	})
	backend := newAppBackend(emit, appOpenInProcess)
	defer func() {
		releaseTitle.Do(func() { close(titleGate) })
		resumeOnce.Do(func() { close(resume) })
		_ = backend.Close()
	}()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace, Text: "repair the tokenizer"})
	if err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 10*time.Second, appTurnDone)
	releaseTitle.Do(func() { close(titleGate) })
	select {
	case <-paused:
	case <-time.After(2 * time.Second):
		t.Fatal("generated name never reached actual outbox writer")
	}
	changed, err := backend.Rename(agent.ID, "My tokenizer work")
	if err != nil || changed.Title != "My tokenizer work" {
		t.Fatalf("rename=%+v error=%v", changed, err)
	}
	if changed.LastActiveAt.IsZero() || changed.LastActiveAt != changed.UpdatedAt {
		t.Fatalf("rename lost last-active fallback: %+v", changed)
	}
	resumeOnce.Do(func() { close(resume) })
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	last := ""
	for _, data := range rec.data {
		if change, ok := data.(appwire.AgentChanged); ok && change.Agent.ID == agent.ID {
			last = change.Agent.Title
			t.Logf("published title: %q", last)
		}
	}
	if last != "My tokenizer work" {
		t.Fatalf("last published name=%q, manual name was overtaken", last)
	}
}

// Subscription ownership must be complete before a dying ask lane can start
// finish. Checking inside the actual WatchTitle door catches publication first.
type intentLifetimeConversation struct {
	*fakeConversation
	backend          *appBackend
	owner            *appAgent
	publishedAtWatch bool
	stopped          chan struct{}
}

func (c *intentLifetimeConversation) WatchTitle() (<-chan session.Event, func()) {
	c.backend.locked(func() { c.publishedAtWatch = c.owner.live != nil })
	lane := make(chan session.Event)
	var once sync.Once
	return lane, func() { once.Do(func() { close(lane); close(c.stopped) }) }
}
func (c *intentLifetimeConversation) WatchQuestions() (<-chan session.Event, func()) {
	lane := make(chan session.Event)
	close(lane) // the real hold/asks path observes immediate connection death
	return lane, func() {}
}
func TestAppTitleCleanupOwnedBeforeImmediateConnectionDeath(t *testing.T) {
	b := newFakeBackend(newAppRecorder(), nil)
	defer b.Close()
	a := &appAgent{view: appwire.Agent{ID: "title-lifetime", Title: "initial"}}
	b.locked(func() { b.agents[a.view.ID] = a })
	c := &intentLifetimeConversation{fakeConversation: &fakeConversation{}, backend: b, owner: a, stopped: make(chan struct{})}
	released := make(chan struct{})
	b.hold(a, &appConn{Conv: c, Release: func() { close(released) }}, t.TempDir())
	if c.publishedAtWatch {
		t.Fatal("title subscription registered after live publication")
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("dead connection was not released")
	}
	select {
	case <-c.stopped:
	case <-time.After(time.Second):
		t.Fatal("title subscription leaked on immediate death")
	}
}
