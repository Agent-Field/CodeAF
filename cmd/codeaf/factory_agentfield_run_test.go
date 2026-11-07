package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/agentfield"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── a fake control plane ────────────────────────────────────────────────────

// afFakeCP is the control plane's side of a run: a tree the test moves, and a
// relay that holds each execution's entries until they are acked, exactly as
// the pipe does.
type afFakeCP struct {
	mu         sync.Mutex
	tree       agentfield.TreeNode
	held       map[string][]agentfield.RelayEntry
	relayDown  int // this many Relay calls fail first
	ackDown    int // this many RelayAck calls fail first
	dispatched []map[string]any
	owner      string
	cancelled  []string
	steers     []string
	steerErr   error
}

func (f *afFakeCP) Dispatch(_ context.Context, target string, input any, owner string) (agentfield.Dispatched, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, _ := json.Marshal(input)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["_target"] = target
	f.dispatched = append(f.dispatched, m)
	f.owner = owner
	return agentfield.Dispatched{ExecutionID: "exec-root", RunID: "run-1", Status: "running"}, nil
}

func (f *afFakeCP) Tree(context.Context, string) (agentfield.TreeNode, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, _ := json.Marshal(f.tree)
	var copied agentfield.TreeNode
	_ = json.Unmarshal(raw, &copied)
	return copied, f.tree.Status, nil
}

func (f *afFakeCP) Relay(_ context.Context, exec string, after int64) ([]agentfield.RelayEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.relayDown > 0 {
		f.relayDown--
		return nil, errors.New("connection refused")
	}
	var out []agentfield.RelayEntry
	for _, e := range f.held[exec] {
		if e.Seq > after {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *afFakeCP) RelayAck(_ context.Context, exec string, through int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ackDown > 0 {
		f.ackDown--
		return errors.New("connection refused")
	}
	var keep []agentfield.RelayEntry
	for _, e := range f.held[exec] {
		if e.Seq > through {
			keep = append(keep, e)
		}
	}
	f.held[exec] = keep
	return nil
}

func (f *afFakeCP) Steer(_ context.Context, exec, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.steerErr != nil {
		return f.steerErr
	}
	f.steers = append(f.steers, exec+": "+message)
	return nil
}

func (f *afFakeCP) CancelTree(_ context.Context, run string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelled = append(f.cancelled, run)
	return nil
}

// relayLines holds a codeaf run's record as the node relays it: started, one
// line per record line, ended.
func (f *afFakeCP) relayLines(exec string, lines [][]byte, ended bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.held == nil {
		f.held = map[string][]agentfield.RelayEntry{}
	}
	seq := int64(0)
	add := func(kind string, data []byte) {
		seq++
		f.held[exec] = append(f.held[exec], agentfield.RelayEntry{Seq: seq, Kind: kind, Data: append(json.RawMessage(nil), data...)})
	}
	add("started", []byte(`{"harness":"codeaf","steerable":true}`))
	for _, l := range lines {
		add("line", l)
	}
	if ended {
		add("ended", []byte(`{}`))
	}
}

func (f *afFakeCP) heldCount(exec string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.held[exec])
}

func afNode(exec, reasoner, status, start, end string, kids ...agentfield.TreeNode) agentfield.TreeNode {
	n := agentfield.TreeNode{ExecutionID: exec, ReasonerID: reasoner, Status: status, StartedAt: start, Children: kids}
	if end != "" {
		n.CompletedAt = &end
	}
	return n
}

// afJob files one RunOn job on a fresh store.
func afJob(t *testing.T) (*store.Store, factory.Item) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{Title: "refund for order 1182 never arrived", Body: "the customer was charged twice", RunOn: "support.handle_ticket", State: factory.StateRunning})
	if err != nil {
		t.Fatal(err)
	}
	return st, it
}

// ── a native step, made by the real session engine ──────────────────────────

// afNativeModel answers a turn with one read of notes.txt and then a sentence.
func afNativeModel(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "no catalog for a test", http.StatusServiceUnavailable)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var env recordedRequest
		_ = json.Unmarshal(raw, &env)
		mu.Lock()
		step := -1
		if len(env.Tools) > 0 {
			step = calls
			calls++
		}
		mu.Unlock()
		if step == 0 {
			writeToolCall(w, env.Stream, "c1", "read", `{"path":"notes.txt"}`)
			return
		}
		writeText(w, env.Stream, "The ledger double-charged order 1182; refund it once.")
	}))
	t.Cleanup(server.Close)
	return server
}

// afNativeStep runs one real turn into a fresh conversation and answers its
// transcript.
func afNativeStep(t *testing.T, workspace string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(workspace, "notes.txt"), []byte("order 1182: charged twice\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	model := afNativeModel(t)
	file, err := appMint(workspace)
	if err != nil {
		t.Fatal(err)
	}
	sources := modelsource.NewSet(modelsource.Connected{Source: modelsource.DefaultSource(model.URL), Key: "test-key", Address: model.URL})
	agent, err := session.New(session.Config{Workspace: workspace, Place: session.Place{Dir: session.DirOf(file)}, SessionFile: file, Model: "openai/gpt-4.1-mini", Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	events, err := agent.Submit(context.Background(), "find why order 1182 was charged twice")
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	return file
}

func afRecordLines(t *testing.T, file string) [][]byte {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var out [][]byte
	for _, l := range bytes.Split(raw, []byte("\n")) {
		if len(bytes.TrimSpace(l)) > 0 {
			out = append(out, l)
		}
	}
	return out
}

// afEntriesJSON is a conversation's entries exactly as agents.entries reads
// them, under one agent id so two conversations can be compared byte for byte.
func afEntriesJSON(t *testing.T, file string) []byte {
	t.Helper()
	record := session.ReadTranscript(file)
	at := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	entries, _ := appEntryShapes("step", appRecordItems(record), at, nil)
	raw, err := json.MarshalIndent(entries, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// THE OWNER'S LAW: a harness step streamed from another machine reads EXACTLY
// as a step that ran here. The node relays the record of a real session turn
// line by line; the factory writes it into a conversation of its own; the
// entries agents.entries reads from it are byte-identical to the native
// step's, tool card and all.
func TestAnAgentFieldStepReadsExactlyLikeANativeStep(t *testing.T) {
	appDayProfile(t)
	workspace := t.TempDir()
	native := afNativeStep(t, workspace)
	lines := afRecordLines(t, native)
	if len(lines) < 4 {
		t.Fatalf("the native turn wrote %d lines; it should have a header, a message, a tool call and an answer", len(lines))
	}

	st, it := afJob(t)
	cp := &afFakeCP{tree: afNode("exec-root", "handle_ticket", "running", "2026-11-07T12:00:00Z", "",
		afNode("exec-inv", "investigate", "running", "2026-11-07T12:00:01Z", ""))}
	cp.relayLines("exec-inv", lines, false)
	r := &afRemote{items: st, floor: st, workspace: workspace, conn: func() (afClient, string, bool) { return cp, "workaf", true }}
	if ended, _ := r.sync(context.Background(), cp, it.ID, cp.tree); ended {
		t.Fatal("the run ended while it was running")
	}
	got, _ := st.Get(it.ID)
	if got.Stream == nil || len(got.Stream.Phases) != 1 || got.Stream.Phases[0].Chat == "" {
		t.Fatalf("the harness step has no chat: %+v", got.Stream)
	}
	mirror := got.Stream.Phases[0].Chat
	if mirror == native {
		t.Fatal("the mirror is the native conversation itself")
	}
	want, have := afEntriesJSON(t, native), afEntriesJSON(t, mirror)
	if !bytes.Equal(want, have) {
		t.Fatalf("the AgentField step's entries differ from the native step's:\nnative:\n%s\nmirror:\n%s", want, have)
	}
	var kinds []string
	var entries []appwire.Entry
	_ = json.Unmarshal(have, &entries)
	for _, e := range entries {
		kinds = append(kinds, e.Kind)
	}
	if !strings.Contains(strings.Join(kinds, ","), "tool") {
		t.Fatalf("the step's entries carry no tool card: %v", kinds)
	}
	// Every line after the header is the node's own, byte for byte.
	mirrored := afRecordLines(t, mirror)
	if len(mirrored) != len(lines) {
		t.Fatalf("the mirror has %d lines, the record %d", len(mirrored), len(lines))
	}
	for i := 1; i < len(lines); i++ {
		if !bytes.Equal(lines[i], mirrored[i]) {
			t.Fatalf("line %d was not written verbatim:\n%s\n%s", i, lines[i], mirrored[i])
		}
	}
	// The header is this conversation's own.
	var header struct{ ID string }
	_ = json.Unmarshal(mirrored[0], &header)
	if header.ID != appAgentID(mirror) {
		t.Fatalf("the mirror's header names %q, its folder is %q", header.ID, appAgentID(mirror))
	}
	// And the conversation is the job's, as a native step's is.
	if owner, ok := readFactoryOwner(mirror); !ok || owner.Job != it.ID || owner.Step != "investigate" {
		t.Fatalf("the mirror is not owned by job %d's investigate step: %+v", it.ID, owner)
	}
}

// WORKAF IS THE SYSTEM OF RECORD and the control plane only a pipe: a factory
// that cannot reach the control plane for a while, and then loses an ack,
// still ends with every line exactly once, and once it has acked, the control
// plane holds nothing for the step.
func TestAnOfflineFactoryGetsEveryLineOnceAndThePipeEmpties(t *testing.T) {
	appDayProfile(t)
	workspace := t.TempDir()
	st, it := afJob(t)
	var lines [][]byte
	for i := range 40 {
		lines = append(lines, []byte(`{"type":"message","role":"assistant","content":"line `+string(rune('a'+i%26))+`","timestamp":"2026-11-07T12:00:00Z"}`))
	}
	header := []byte(`{"type":"session","version":1,"id":"nodefolder","cwd":"/node/work","model":"deepseek/deepseek-v4.1-flash","timestamp":"2026-11-07T12:00:00Z"}`)
	cp := &afFakeCP{relayDown: 3, ackDown: 1, tree: afNode("exec-root", "handle_ticket", "running", "2026-11-07T12:00:00Z", "",
		afNode("exec-inv", "investigate", "running", "2026-11-07T12:00:01Z", ""))}
	cp.relayLines("exec-inv", append([][]byte{header}, lines...), true)
	r := &afRemote{items: st, floor: st, workspace: workspace, conn: func() (afClient, string, bool) { return cp, "workaf", true }}
	for range 8 {
		r.sync(context.Background(), cp, it.ID, cp.tree)
	}
	got, _ := st.Get(it.ID)
	mirror := got.Stream.Phases[0].Chat
	kept := afRecordLines(t, mirror)
	if len(kept) != 1+len(lines) {
		t.Fatalf("the conversation has %d lines; want the header and %d lines once each", len(kept), len(lines))
	}
	for i, l := range lines {
		if !bytes.Equal(kept[i+1], l) {
			t.Fatalf("line %d is %s, want %s", i+1, kept[i+1], l)
		}
	}
	if n := cp.heldCount("exec-inv"); n != 0 {
		t.Fatalf("the control plane still holds %d entries after the ack", n)
	}
	state, ok := afReadRelay(mirror)
	if !ok || state.Acked != int64(len(lines)+3) || state.Steerable {
		t.Fatalf("the relay record is %+v; want every seq kept and the ended run not steerable", state)
	}

	// A factory that restarts reads from its record: a fresh reader asks the
	// control plane only for what came after it, and writes nothing twice.
	cp.mu.Lock()
	cp.held["exec-inv"] = []agentfield.RelayEntry{{Seq: 5, Kind: "line", Data: lines[3]}}
	cp.mu.Unlock()
	again := &afRemote{items: st, floor: st, workspace: workspace, conn: r.conn}
	again.sync(context.Background(), cp, it.ID, cp.tree)
	if n := len(afRecordLines(t, mirror)); n != 1+len(lines) {
		t.Fatalf("a restarted reader wrote a line twice: %d lines", n)
	}
}

// THE RAIL IS THE TREE. Every execution is one step of the job, each after
// the step that called it; a reasoner called more than once gets a unique
// name with the reasoner as its label; siblings that ran at once share a
// group; and factory.agentfield_run folds them back into the tree.
func TestAnAgentFieldRunFoldsIntoStepsAndBack(t *testing.T) {
	root := afNode("exec-root", "research", "running", "2026-11-07T12:00:00Z", "",
		afNode("e-plan", "plan", "succeeded", "2026-11-07T12:00:01Z", "2026-11-07T12:00:05Z"),
		afNode("e-i2", "investigate", "succeeded", "2026-11-07T12:00:06Z", "2026-11-07T12:00:25Z",
			afNode("e-s", "search_docs", "succeeded", "2026-11-07T12:00:07Z", "2026-11-07T12:00:08Z")),
		afNode("e-i1", "investigate", "running", "2026-11-07T12:00:06Z", ""),
		afNode("e-i3", "investigate", "failed", "2026-11-07T12:00:06.5Z", "2026-11-07T12:00:09Z"),
		afNode("e-syn", "synthesize", "pending", "", ""),
	)
	steps := afFlatten(root)
	var got []string
	for _, s := range steps {
		got = append(got, s.name+"|"+s.label+"|"+s.parent+"|"+s.group)
	}
	want := []string{
		"plan|||",
		"investigate1|investigate||investigate1",
		"searchdocs|search_docs|investigate1|",
		"investigate2|investigate||investigate1",
		"investigate3|investigate||investigate1",
		"synthesize|||",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the run flattened to\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	appDayProfile(t)
	st, it := afJob(t)
	cp := &afFakeCP{tree: root}
	r := &afRemote{items: st, floor: st, workspace: t.TempDir(), conn: func() (afClient, string, bool) { return cp, "workaf", true }}
	r.sync(context.Background(), cp, it.ID, root)
	job, _ := st.Get(it.ID)
	if job.Stream.Cur != 3 {
		t.Fatalf("the job stands at step %d; the first running top-level step is 3", job.Stream.Cur)
	}
	states := map[string]factory.PhaseState{}
	for _, ph := range job.Stream.Phases {
		states[ph.Name] = ph.State
	}
	if states["plan"] != factory.PhaseDone || states["investigate1"] != factory.PhaseDone || states["investigate2"] != factory.PhaseRunning || states["investigate3"] != factory.PhaseFailed || states["synthesize"] != factory.PhasePending {
		t.Fatalf("the steps' states are %v", states)
	}
	tree := afRunTree(job)
	var top []string
	for _, s := range tree.Steps {
		top = append(top, s.Name)
	}
	if strings.Join(top, ",") != "plan,investigate1,investigate2,investigate3,synthesize" {
		t.Fatalf("the tree's top level is %v", top)
	}
	inv := tree.Steps[1]
	if len(inv.Children) != 1 || inv.Children[0].Name != "searchdocs" || inv.Children[0].Label != "search_docs" || inv.Group != "investigate1" || inv.Label != "investigate" {
		t.Fatalf("investigate1 is %+v", inv)
	}
	// A plain step has no chat, so no agent and no steer.
	if tree.Steps[0].Agent != "" || tree.Steps[0].Steerable {
		t.Fatalf("a plain step has a chat: %+v", tree.Steps[0])
	}
	raw, _ := json.Marshal(tree.Steps[0])
	for _, absent := range []string{`"agent"`, `"steerable"`, `"group"`, `"label"`, `"execution_id"`} {
		if strings.Contains(string(raw), absent) {
			t.Fatalf("an unknown field is on the wire (the emptiness law): %s", raw)
		}
	}
}

// THE JOB IS THE RUN: Start dispatches the job's reasoner with the job, the
// job's steps follow the tree, and the job ends when the run does — done on
// success, with the run's own words on failure, and the person's Stop cancels
// the whole tree.
func TestARunOnJobDispatchesFollowsAndEndsWithItsRun(t *testing.T) {
	appDayProfile(t)
	prev := afRemoteEvery
	afRemoteEvery = 10 * time.Millisecond
	t.Cleanup(func() { afRemoteEvery = prev })

	for _, tc := range []struct {
		name, status, reason string
		cancel               bool
		want                 string
	}{
		{name: "succeeds", status: "succeeded"},
		{name: "fails", status: "failed", reason: "investigate: the ledger is unreachable", want: "the AgentField run failed: investigate: the ledger is unreachable"},
		{name: "is stopped", cancel: true, want: context.Canceled.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, it := afJob(t)
			cp := &afFakeCP{tree: afNode("exec-root", "handle_ticket", "running", "2026-11-07T12:00:00Z", "",
				afNode("e-t", "triage", "succeeded", "2026-11-07T12:00:01Z", "2026-11-07T12:00:02Z"))}
			r := &afRemote{items: st, floor: st, workspace: t.TempDir(), conn: func() (afClient, string, bool) { return cp, "workaf-node", true }}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- r.Run(ctx, it.ID) }()
			time.Sleep(50 * time.Millisecond)
			if tc.cancel {
				cancel()
			} else {
				cp.mu.Lock()
				cp.tree.Status = tc.status
				if tc.reason != "" {
					cp.tree.StatusReason = &tc.reason
				}
				cp.mu.Unlock()
			}
			var err error
			select {
			case err = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the run never ended")
			}
			if (err == nil) != (tc.want == "") || (err != nil && err.Error() != tc.want) {
				t.Fatalf("the run ended with %v, want %q", err, tc.want)
			}
			cp.mu.Lock()
			defer cp.mu.Unlock()
			if len(cp.dispatched) != 1 || cp.dispatched[0]["_target"] != "support.handle_ticket" || cp.dispatched[0]["title"] != it.Title || cp.dispatched[0]["body"] != it.Body || cp.owner != "workaf-node" {
				t.Fatalf("dispatched %+v as %q", cp.dispatched, cp.owner)
			}
			if tc.cancel != (len(cp.cancelled) == 1 && cp.cancelled[0] == "run-1") {
				t.Fatalf("cancelled %v", cp.cancelled)
			}
			job, _ := st.Get(it.ID)
			if job.AgentField == nil || job.AgentField.ExecutionID != "exec-root" || len(job.Stream.Phases) != 1 || job.Stream.Phases[0].Name != "triage" {
				t.Fatalf("the job does not carry its run: %+v %+v", job.AgentField, job.Stream)
			}
		})
	}
}

// A RUN ON JOB ON A FACTORY WITH NO AGENTFIELD is refused with words.
func TestARunOnJobWithNoAgentFieldIsRefused(t *testing.T) {
	appDayProfile(t)
	st, it := afJob(t)
	r := &afRemote{items: st, floor: st, conn: func() (afClient, string, bool) { return nil, "", false }}
	if err := r.Run(context.Background(), it.ID); err == nil || err.Error() != factoryrun.NoRemoteWords {
		t.Fatalf("got %v", err)
	}
}

// STEERING IS THE NORMAL SEND, AND THE CODEAF HARNESS'S ALONE. A person's
// words to a working harness step go to its execution; to one that has ended
// they are refused; and a conversation that is no AgentField step is not
// touched at all.
func TestASendToAnAgentFieldStepIsASteer(t *testing.T) {
	appDayProfile(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(file, []byte(`{"type":"session","version":1,"id":"x"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := newAppBackend(newAppRecorder(), nil)
	t.Cleanup(func() { _ = b.Close() })
	a := &appAgent{transcript: file}

	if handled, _ := b.agentFieldSteer(a, "check negative amounts too"); handled {
		t.Fatal("a conversation that is no AgentField step was taken as one")
	}
	m := &afMirror{file: file, id: "x", state: afRelayState{ExecutionID: "exec-inv", Step: "investigate", Steerable: false}}
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	handled, err := b.agentFieldSteer(a, "check negative amounts too")
	if !handled || err == nil || !strings.Contains(err.Error(), afSteerEnded) {
		t.Fatalf("a steer to an ended step: handled=%v err=%v", handled, err)
	}
	var wire *appwire.Error
	if !errors.As(err, &wire) || wire.Code != appwire.CodeConflict {
		t.Fatalf("the refusal is not a conflict: %#v", err)
	}
	m.state.Steerable = true
	_ = m.save()
	if handled, err := b.agentFieldSteer(a, "check negative amounts too"); !handled || err == nil || !strings.Contains(err.Error(), afSteerNoFactory) {
		t.Fatalf("a steer with no factory: handled=%v err=%v", handled, err)
	}
	if got := afSteerWords(errors.New(`steer: 409 {"error":"this execution is not running the codeaf harness, so it cannot be steered"}`)); got != "this step is not running the codeaf harness, so it cannot be steered" {
		t.Fatalf("the node's refusal reads %q", got)
	}
}

// THE WHOLE ROAD THROUGH THE FACTORY'S OWN RUNNER: a job filed with run_on and
// no repository is launched by the owner's ordinary Launch, handed to its
// AgentField run, carries that run's steps, and is done when the run is.
func TestTheFactoryRunnerHandsARunOnJobToItsRun(t *testing.T) {
	appDayProfile(t)
	prev := afRemoteEvery
	afRemoteEvery = 10 * time.Millisecond
	t.Cleanup(func() { afRemoteEvery = prev })
	g := newRunRig(t)
	cp := &afFakeCP{tree: afNode("exec-root", "handle_ticket", "running", "2026-11-07T12:00:00Z", "",
		afNode("e-t", "triage", "succeeded", "2026-11-07T12:00:01Z", "2026-11-07T12:00:02Z"),
		afNode("e-i", "investigate", "running", "2026-11-07T12:00:03Z", ""))}
	remote := &afRemote{items: g.st, floor: g.st, workspace: g.workspace, conn: func() (afClient, string, bool) { return cp, "workaf", true }}
	r := buildFactoryRunnerWith(g.st, g.st, g.workspace, t.TempDir(), nil, nil, remote.Run)
	stopAll(t, g.st, r)
	id, err := g.st.Add(context.Background(), factory.Item{Title: "refund never arrived", Body: "order 1182", Tier: factory.TierOwner, RunOn: "support.handle_ticket"})
	if err != nil {
		t.Fatal(err)
	}
	if err := factory.LocalSeam(g.st, time.Now(), factory.WithRunner(r)).Launch(id); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		it, _ := g.st.Get(id)
		if it.Stream != nil && len(it.Stream.Phases) == 2 && it.Stream.Phases[1].State == factory.PhaseRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the job never carried its run's steps: %+v", it.Stream)
		}
		time.Sleep(5 * time.Millisecond)
	}
	cp.mu.Lock()
	cp.tree.Status, cp.tree.Children[1].Status = "succeeded", "succeeded"
	end := "2026-11-07T12:00:09Z"
	cp.tree.Children[1].CompletedAt = &end
	cp.mu.Unlock()
	it := g.waitFor(t, id, factory.StateShipped)
	if it.Stream.Phases[0].Name != "triage" || it.Stream.Phases[1].Name != "investigate" || it.Stream.Phases[1].State != factory.PhaseDone {
		t.Fatalf("the job's steps are %+v", it.Stream.Phases)
	}
}
