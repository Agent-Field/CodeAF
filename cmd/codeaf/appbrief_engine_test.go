package main

// A BRIEF IS ANSWERED BY ITS TYPED RECEIPT, ACROSS THE SOCKET THE APP REALLY USES.
//
// Every app conversation is a remote.Agent, so an engine's error reaches
// `tasks.brief` as bare text: no sentinel survives the crossing, and a brief
// whose effect may have landed used to come back as an ordinary error that
// invited a second send. These tests drive the registered app server's
// `tasks.brief` through the backend, remote.Loopback and a real session engine
// (session.New) holding a real plan store, with SQLite itself refusing the
// brief's COMMIT (plandbtest), a reply lost after the real effect, and an engine
// that cannot type the outcome. Nothing in the production chain is replaced.

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/plandb/plandbtest"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// briefEngine is the real engine behind the socket with its two task doors
// counted. afterEffect, when set, runs after the real receipt door has
// returned and before its reply is written; script, when set, answers the
// receipt door in its place for an outcome a real engine cannot be made to give
// from outside (a delivered brief whose page brief was not kept).
type briefEngine struct {
	*session.Agent
	plain, receipts atomic.Int32
	afterEffect     func()
	mu              sync.Mutex
	script          []session.AppTaskReceipt
}

func (e *briefEngine) AppTaskAction(intent session.AppTaskIntent) error {
	e.plain.Add(1)
	return e.Agent.AppTaskAction(intent)
}

func (e *briefEngine) AppTaskActionReceipt(intent session.AppTaskIntent) session.AppTaskReceipt {
	e.receipts.Add(1)
	e.mu.Lock()
	if len(e.script) > 0 {
		receipt := e.script[0]
		e.script = e.script[1:]
		e.mu.Unlock()
		return receipt
	}
	e.mu.Unlock()
	receipt := e.Agent.AppTaskActionReceipt(intent)
	if e.afterEffect != nil {
		e.afterEffect()
	}
	return receipt
}

// oldBriefEngine is an engine with no receipt door: the real engine's task
// doors and nothing that types an outcome, so its welcome says it cannot.
type oldBriefEngine struct {
	remote.WrappedAgent
	agent *session.Agent
	plain atomic.Int32
}

func (e *oldBriefEngine) AppTasks() ([]session.AppTaskView, error) { return e.agent.AppTasks() }
func (e *oldBriefEngine) AppTask(id string) (session.AppTaskView, bool, error) {
	return e.agent.AppTask(id)
}
func (e *oldBriefEngine) AppTaskAction(intent session.AppTaskIntent) error {
	e.plain.Add(1)
	return e.agent.AppTaskAction(intent)
}
func (e *oldBriefEngine) AppQuickTasks() ([]session.AppQuickTaskView, error) {
	return e.agent.AppQuickTasks()
}
func (e *oldBriefEngine) AppQuickTaskAction(intent session.AppTaskIntent) ([]string, []string, error) {
	return e.agent.AppQuickTaskAction(intent)
}
func (e *oldBriefEngine) MoveTurnToTask() error { return e.agent.MoveTurnToTask() }

const briefOriginal = "replace the lookup"

// briefRig is one real engine over one real plan store, its plan task's app
// id, and the store's path.
type briefRig struct {
	engine *session.Agent
	path   string
	local  string
}

// newBriefRig builds a session engine the public way, in a session folder
// holding a plan store seeded with one task under the folder's own chat tag,
// which is how the engine's pages adopt a store. gate, when set, is armed
// against the path before the engine first opens the store.
func newBriefRig(t *testing.T, gate func(path string)) briefRig {
	t.Helper()
	model := newHeldModel(t)
	t.Cleanup(func() { close(model.release) })
	sources := modelsource.NewSet(modelsource.Connected{
		Source: modelsource.DefaultSource(model.server.URL), Key: "test-key", Address: model.server.URL,
	})
	place := session.Place{Dir: t.TempDir()}
	path := filepath.Join(place.Dir, "plandb.db")
	if gate != nil {
		gate(path)
	}
	seed, err := plandb.Open(path, "the run", "root", "The run", "drive the plan", place.ID())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.AddMany([]plandb.TaskSpec{{ID: "child", Title: "Write handler", Description: briefOriginal}}); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	config := session.Config{Workspace: t.TempDir(), Place: place, Model: "openai/gpt-4.1-mini", Sources: sources,
		SessionFile: filepath.Join(place.Dir, "session.jsonl")}
	engine, err := session.New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	rows, err := engine.AppTasks()
	if err != nil {
		t.Fatal(err)
	}
	local := ""
	for _, row := range rows {
		if row.Title == "Write handler" {
			local = row.ID
		}
	}
	if local == "" {
		t.Fatalf("the engine's pages do not list the plan task: %+v", rows)
	}
	return briefRig{engine: engine, path: path, local: local}
}

// description reads the plan task's description from the disk through a
// handle of its own.
func (r briefRig) description(t *testing.T) string {
	t.Helper()
	store, err := plandb.Open(r.path, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	task := store.Task("child")
	if task == nil {
		t.Fatal("the plan task is gone")
	}
	return task.Description
}

// briefWire serves the registered app server over the backend whose one
// conversation is the near end of a loopback to agent, and answers the wire's
// own error code for one call ("" for success).
type briefWire struct {
	t       *testing.T
	in      io.Writer
	scanner *bufio.Scanner
	next    int
	agentID string
	loop    *remote.Loop
}

func newBriefWire(t *testing.T, agent remote.WrappedAgent) *briefWire {
	t.Helper()
	loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
		return &remote.Engine{Agent: agent}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	backend, _, wireAgent := workBackend(t, &fakeConversation{})
	backend.locked(func() { backend.agents[wireAgent.ID].live.conn.Conv = loop.Client.Agent() })
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	server := appwire.NewServer(outW)
	go func() {
		_ = server.Serve(context.Background(), inR, appwire.Hello{Version: "test"}, backend)
		outW.Close()
	}()
	t.Cleanup(func() { inW.Close() })
	scanner := bufio.NewScanner(outR)
	scanner.Buffer(make([]byte, 4<<20), 4<<20)
	if !scanner.Scan() {
		t.Fatal("no hello")
	}
	return &briefWire{t: t, in: inW, scanner: scanner, agentID: wireAgent.ID, loop: loop}
}

func (w *briefWire) brief(local, text string) string {
	w.t.Helper()
	w.next++
	params, _ := json.Marshal(map[string]string{"id": w.agentID + ":" + local, "text": text})
	if _, err := io.WriteString(w.in, `{"id":`+strconv.Itoa(w.next)+`,"method":"tasks.brief","params":`+string(params)+"}\n"); err != nil {
		w.t.Fatal(err)
	}
	var answer struct {
		ID    int `json:"id"`
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	for w.scanner.Scan() {
		if err := json.Unmarshal(w.scanner.Bytes(), &answer); err == nil && answer.ID == w.next {
			break
		}
	}
	if answer.Error != nil {
		return answer.Error.Code
	}
	return ""
}

// A BRIEF WHOSE COMMIT SQLITE REFUSED MAY BE ON THE DISK: `uncertain`, and it is
// never sent again by any door. A brief to a task that is not there is refused
// before any commit; the next brief lands exactly once.
func TestABriefAcrossTheSocketWhoseCommitFailsIsUncertainAndNeverSentTwice(t *testing.T) {
	const armed = "brief-commit: keep the old lookup as a fallback"
	var gate *plandbtest.Gate
	rig := newBriefRig(t, func(path string) { gate = plandbtest.GateTask(t, path, armed) })
	engine := &briefEngine{Agent: rig.engine}
	wire := newBriefWire(t, engine)

	if code := wire.brief("missing", armed); code != appwire.CodeNotFound || gate.Refused() != 0 {
		t.Fatalf("a brief to nothing = %q (commits refused %d), want not_found before any commit", code, gate.Refused())
	}
	if code := wire.brief(rig.local, armed); code != appwire.CodeUncertain {
		t.Fatalf("a brief whose commit SQLite refused = %q, want uncertain", code)
	}
	if gate.Refused() != 1 || engine.receipts.Load() != 2 || engine.plain.Load() != 0 {
		t.Fatalf("commits refused %d, receipt asks %d, plain asks %d; want 1, 2, 0", gate.Refused(), engine.receipts.Load(), engine.plain.Load())
	}
	if got := rig.description(t); got != briefOriginal {
		t.Fatalf("the description after the refused commit = %q", got)
	}

	gate.Disarm()
	if code := wire.brief(rig.local, "brief-after: log each fallback"); code != "" {
		t.Fatalf("a brief after the gate was disarmed = %q", code)
	}
	if got := rig.description(t); got != "brief-after: log each fallback\n\n"+briefOriginal {
		t.Fatalf("the description after the healthy brief = %q, want it once and the refused one never", got)
	}
	if engine.receipts.Load() != 3 || engine.plain.Load() != 0 {
		t.Fatalf("receipt asks %d, plain asks %d; want 3, 0", engine.receipts.Load(), engine.plain.Load())
	}
}

// A BRIEF WHOSE REPLY NEVER CAME, OR WHOSE ENGINE CANNOT TYPE IT, IS UNCERTAIN,
// and neither is followed by the plain door or a second send.
func TestABriefWhoseReplyIsLostOrWhoseEngineCannotTypeItIsUncertain(t *testing.T) {
	t.Run("the reply is lost after the real effect", func(t *testing.T) {
		rig := newBriefRig(t, nil)
		engine := &briefEngine{Agent: rig.engine}
		wire := newBriefWire(t, engine)
		engine.afterEffect = func() { _ = wire.loop.Client.Close() }
		if code := wire.brief(rig.local, "brief-lost: the link went"); code != appwire.CodeUncertain {
			t.Fatalf("a brief whose reply was lost = %q, want uncertain", code)
		}
		if got := rig.description(t); got != "brief-lost: the link went\n\n"+briefOriginal {
			t.Fatalf("the description = %q, want the brief exactly once", got)
		}
		if engine.receipts.Load() != 1 || engine.plain.Load() != 0 {
			t.Fatalf("receipt asks %d, plain asks %d; want 1, 0", engine.receipts.Load(), engine.plain.Load())
		}
	})
	t.Run("the engine has no receipt door", func(t *testing.T) {
		rig := newBriefRig(t, nil)
		engine := &oldBriefEngine{WrappedAgent: rig.engine, agent: rig.engine}
		wire := newBriefWire(t, engine)
		if code := wire.brief(rig.local, "brief-old: no typed answer"); code != appwire.CodeUncertain {
			t.Fatalf("a brief to an engine that cannot type it = %q, want uncertain", code)
		}
		if engine.plain.Load() != 0 {
			t.Fatalf("the plain door was asked %d times after the typed one could not answer", engine.plain.Load())
		}
		if got := rig.description(t); got != briefOriginal {
			t.Fatalf("the description = %q, want nothing sent", got)
		}
	})
}

// THE TYPED OUTCOMES A REAL ENGINE CANNOT BE MADE TO GIVE FROM OUTSIDE cross the
// same socket: a delivered brief whose page brief was not kept is uncertain (its
// producer is proven in internal/session by
// TestADeliveredBriefReportsWhenOnlyItsPageMetadataCouldNotBeKept), a held
// brief is taken, and a typed refusal keeps its code. Each is one ask.
func TestABriefIsAnsweredByItsTypedReceiptAndNeverSentTwice(t *testing.T) {
	rig := newBriefRig(t, nil)
	engine := &briefEngine{Agent: rig.engine, script: []session.AppTaskReceipt{
		{Outcome: session.AppTaskDelivered, BriefNotKept: true, Reason: session.ErrBriefNotKept.Error()},
		{Outcome: session.AppTaskHeld},
		{Outcome: session.AppTaskRefused, Reason: "task 9 has finished, not running"},
	}}
	wire := newBriefWire(t, engine)
	for i, want := range []string{appwire.CodeUncertain, "", appwire.CodeBadParams} {
		if code := wire.brief(rig.local, "scripted words"); code != want {
			t.Fatalf("answer %d = %q, want %q", i, code, want)
		}
		if engine.receipts.Load() != int32(i+1) || engine.plain.Load() != 0 {
			t.Fatalf("after answer %d: receipt asks %d, plain asks %d", i, engine.receipts.Load(), engine.plain.Load())
		}
	}
	if got := rig.description(t); got != briefOriginal {
		t.Fatalf("a scripted answer reached the store: %q", got)
	}
}
