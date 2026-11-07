package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	// afterCancel runs once CancelTree has marked the run cancelled: a line
	// the node relays after the person's stop.
	afterCancel func()
	// approval is each execution's pending approval, as approval-status
	// reports it; resolved records every answer posted to approval-response.
	approval map[string]agentfield.Approval
	resolved []string
	result   json.RawMessage
}

func (f *afFakeCP) ApprovalStatus(_ context.Context, exec string) (agentfield.Approval, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.approval[exec]
	if !ok {
		return agentfield.Approval{}, errors.New("no_approval_request")
	}
	return a, nil
}

func (f *afFakeCP) ResolveApproval(_ context.Context, exec, decision, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolved = append(f.resolved, exec+": "+decision+": "+reason)
	a := f.approval[exec]
	a.Status = decision
	if f.approval == nil {
		f.approval = map[string]agentfield.Approval{}
	}
	f.approval[exec] = a
	return nil
}

// Nodes is the control plane's nodes, as GET /api/v1/nodes lists them.
func (f *afFakeCP) Nodes(context.Context) ([]agentfield.NodeReasoners, error) {
	return []agentfield.NodeReasoners{{ID: "swe-planner", HealthStatus: "active", Reasoners: []agentfield.Reasoner{{ID: "build_issue", Description: "build an issue",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"},"issue_number":{"type":"integer"}},"required":["title"]}`)}}}}, nil
}

// Result is the run's output, as GET /api/v1/executions/<id> reports it.
func (f *afFakeCP) Result(context.Context, string) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.result, nil
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

// RunURL is the run's page, as the real client names it.
func (f *afFakeCP) RunURL(run string) string { return "https://cp.test/ui/runs/" + run }

func (f *afFakeCP) NodeHost(_ context.Context, node string) string {
	if node == "support" {
		return "dumb"
	}
	return ""
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
	f.cancelled = append(f.cancelled, run)
	// The control plane marks the run and every running execution in it.
	f.tree.Status = "cancelled"
	for i := range f.tree.Children {
		if f.tree.Children[i].Status == "running" {
			f.tree.Children[i].Status = "cancelled"
		}
	}
	after := f.afterCancel
	f.mu.Unlock()
	if after != nil {
		after()
	}
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
// name with the reasoner, in words, as its label; siblings that ran at once share a
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
		got = append(got, s.name+"|"+s.label+"|"+s.parent+"|"+s.group+"|"+s.groupLabel)
	}
	want := []string{
		"plan|Plan|||",
		"investigate1|Investigate||investigate1|Investigate",
		"searchdocs|Search docs|investigate1||",
		"investigate2|Investigate||investigate1|Investigate",
		"investigate3|Investigate||investigate1|Investigate",
		"synthesize|Synthesize|||",
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
	if len(inv.Children) != 1 || inv.Children[0].Name != "searchdocs" || inv.Children[0].Label != "Search docs" || inv.Group != "investigate1" || inv.Label != "Investigate" {
		t.Fatalf("investigate1 is %+v", inv)
	}
	// A plain step has no chat, so no agent and no steer.
	if tree.Steps[0].Agent != "" || tree.Steps[0].Steerable {
		t.Fatalf("a plain step has a chat: %+v", tree.Steps[0])
	}
	raw, _ := json.Marshal(tree.Steps[0])
	for _, absent := range []string{`"agent"`, `"steerable"`, `"group"`, `"execution_id"`} {
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
			// The factory's job is floor; a worker runs its own copy of it.
			floor := st
			if tc.cancel {
				floor, _ = afJob(t)
			}
			r := &afRemote{items: st, floor: floor, workspace: t.TempDir(), conn: func() (afClient, string, bool) { return cp, "workaf-node", true }}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- r.Run(ctx, it.ID) }()
			time.Sleep(50 * time.Millisecond)
			if tc.cancel {
				cp.afterCancel = func() {
					cp.relayLines("e-t", [][]byte{[]byte(`{"type":"message","role":"assistant","content":"stopping","timestamp":"2026-11-07T12:00:03Z"}`)}, true)
				}
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
			// The run's page is the job's source link.
			if src, _ := factory.JobSource(job); job.AgentField.URL != "https://cp.test/ui/runs/run-1" || src.URL != job.AgentField.URL {
				t.Fatalf("the job's run page is %q, its source %+v", job.AgentField.URL, src)
			}
			// A stopped run is read to its end: the lines the node relays after
			// the stop are kept and acked, so the control plane holds nothing,
			// and the job records the run as cancelled.
			if tc.cancel {
				if n := len(cp.held["e-t"]); n != 0 {
					t.Fatalf("the control plane still holds %d entries after the stop", n)
				}
				fj, _ := floor.Get(it.ID)
				if fj.AgentField == nil || fj.AgentField.Status != "cancelled" || fj.AgentField.Ended.IsZero() {
					t.Fatalf("the factory's stopped job records its run as %+v", fj.AgentField)
				}
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
	r := buildFactoryRunnerWith(g.st, g.st, g.workspace, t.TempDir(), nil, nil, remote.Run, factoryrun.ExecutorFunc(remote.Step))
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

// THE REAL DOORS, END TO END: on a real local factory behind the real wire,
// a job filed with run_on and no repository is started by factory.job_start,
// leased to this machine's runner, run on AgentField, carries the run's steps
// on factory.job and the harness step's chat on factory.job_chats, answers
// factory.agentfield_run, takes a send to that chat as a steer, and is done
// when the run is.
func TestARunOnJobThroughTheRealFactoryDoors(t *testing.T) {
	prev := afRemoteEvery
	afRemoteEvery = 20 * time.Millisecond
	t.Cleanup(func() { afRemoteEvery = prev })
	s := newLocalSetup(t)
	// A runner with no model key takes no job at all (it attaches needing a
	// provider), so the test's runner has one. The run_on job never uses it.
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")
	// Two lookups run at once, then triage, then investigate (which calls
	// search_docs, plain code, and verify, which runs the harness).
	cp := &afFakeCP{tree: afNode("exec-root", "handle_ticket", "running", "2026-11-07T12:00:00Z", "",
		afNode("e-l1", "lookup", "succeeded", "2026-11-07T12:00:00.2Z", "2026-11-07T12:00:00.6Z"),
		afNode("e-l2", "lookup", "succeeded", "2026-11-07T12:00:00.3Z", "2026-11-07T12:00:00.7Z"),
		// A run step may share a stock step's name: it is still the run's.
		afNode("e-t", "plan", "succeeded", "2026-11-07T12:00:01Z", "2026-11-07T12:00:02Z"),
		afNode("e-i", "investigate", "running", "2026-11-07T12:00:03Z", "",
			afNode("e-s", "search_docs", "succeeded", "2026-11-07T12:00:04Z", "2026-11-07T12:00:05Z"),
			afNode("e-v", "verify", "running", "2026-11-07T12:00:06Z", "")))}
	// A nested execution that runs the harness is a chat at its depth.
	cp.relayLines("e-v", [][]byte{
		[]byte(`{"type":"session","version":1,"id":"nodeverify","cwd":"/node","model":"m","timestamp":"2026-11-07T12:00:06Z"}`),
		[]byte(`{"type":"message","role":"user","content":"verify the fix","timestamp":"2026-11-07T12:00:06Z"}`),
	}, false)
	cp.relayLines("e-i", [][]byte{
		[]byte(`{"type":"session","version":1,"id":"nodefolder","cwd":"/node","model":"m","timestamp":"2026-11-07T12:00:03Z"}`),
		[]byte(`{"type":"message","role":"user","content":"why is the total wrong?","timestamp":"2026-11-07T12:00:03Z"}`),
		[]byte(`{"type":"message","role":"assistant","content":"Looking at the ledger.","timestamp":"2026-11-07T12:00:04Z"}`),
	}, false)
	afTestConn = func() (afClient, string, bool) { return cp, "codeaf-factory", true }
	t.Cleanup(func() { afTestConn = nil })

	// A held chat needs a conversation behind it; the steer never reaches it.
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Transcript: ask.Transcript, Workspace: ask.Workspace, Assembled: true, Release: func() {}}, nil
	}
	st := s.start()
	eventually(t, "the runner holds the worker lock", func() bool { return runnerHeld(s) })
	s.setup("factory.setup_connect", map[string]any{"source": "token", "token": setupSecret})
	s.setup("factory.setup_select", map[string]any{"repos": []string{"acme/api"}})
	s.until("checkouts", func(v appwire.FactorySetupView) bool { return v.State == "checkouts" })
	s.setup("factory.setup_skip", map[string]any{"repo": "acme/api"})
	s.setup("factory.setup_done", map[string]any{})
	s.must("factory.local_run", map[string]any{"caller": "w1"}, &appwire.FactoryLocalStatus{})

	s.backend.local.mu.Lock()
	host := s.backend.local.host
	s.backend.local.mu.Unlock()
	job, err := host.floor.Add(context.Background(), factory.Item{Title: "ledger total wrong", Body: "refund -500 printed 0.-50", Tier: factory.TierOwner, RunOn: "support.handle_ticket"})
	if err != nil {
		t.Fatal(err)
	}
	var read0 struct{ Version int }
	_ = json.Unmarshal(afCall(s, "factory.job", map[string]any{"factory": st.Factory, "job": job}), &read0)
	s.must("factory.job_start", map[string]any{"factory": st.Factory, "job": job, "expect": map[string]any{"version": read0.Version}}, nil)

	var chats appwire.FactoryJobChats
	defer func() {
		if t.Failed() {
			t.Logf("job at failure: %s", afCall(s, "factory.job", map[string]any{"factory": st.Factory, "job": job}))
		}
	}()
	eventually(t, "the harness step has a chat", func() bool {
		chats = appwire.FactoryJobChats{}
		raw := afCall(s, "factory.job_chats", map[string]any{"factory": st.Factory, "job": job})
		_ = json.Unmarshal(raw, &chats)
		return len(chats.Steps) == 2 && chats.Steps[0].Step == "investigate" && chats.Steps[1].Step == "verify"
	})
	// THE DESK READS THE ORDINARY JOB AND NOTHING ELSE (owner's decision):
	// every execution, nested ones too, is a flat step with its parent and
	// group, a name unique in the job, kind "check" for plain code and
	// "chat" for a harness step, whose chat factory.job_chats names.
	var read struct {
		Steps []struct {
			Name, State, Parent, Group, Label, Kind string
			GroupLabel                              string `json:"group_label"`
			InRun                                   bool   `json:"in_run"`
		}
		StepsDone  int    `json:"steps_done"`
		StepsTotal int    `json:"steps_total"`
		Runner     string `json:"runner"`
	}
	raw := afCall(s, "factory.job", map[string]any{"factory": st.Factory, "job": job})
	_ = json.Unmarshal(raw, &read)
	var steps []string
	for _, x := range read.Steps {
		steps = append(steps, x.Name+"/"+x.Kind+"/"+x.State+"/"+x.Parent+"/"+x.Group+"/"+x.GroupLabel+"/"+x.Label)
	}
	// THE NAME IS THE KEY, THE LABEL IS A PERSON'S WORDS: search_docs reads
	// "Search docs", and a group that ran at once is called by what its
	// members share ("Lookup").
	want := []string{
		"lookup1/check/done//lookup1/Lookup/Lookup",
		"lookup2/check/done//lookup1/Lookup/Lookup",
		"plan/check/done////Plan",
		"investigate/chat/running////Investigate",
		"searchdocs/check/done/investigate///Search docs",
		"verify/chat/running/investigate///Verify",
	}
	if strings.Join(steps, "\n") != strings.Join(want, "\n") {
		t.Fatalf("factory.job's steps are\n%s\nwant\n%s\n(%s)", strings.Join(steps, "\n"), strings.Join(want, "\n"), raw)
	}
	if strings.Contains(string(raw), `"note"`) || strings.Contains(string(raw), "write a plan") {
		t.Fatalf("a run step wears a stock step's words: %s", raw)
	}
	if strings.Contains(string(raw), `"runs_on"`) {
		t.Fatalf("a run_on job names a model this factory does not run: %s", raw)
	}
	if read.Runner != "support · dumb" {
		t.Fatalf("the job runs on %q; want the node and its machine", read.Runner)
	}
	if read.StepsDone != 3 || read.StepsTotal != 4 {
		t.Fatalf("the job counts %d / %d; want the four top-level steps, three done", read.StepsDone, read.StepsTotal)
	}

	// factory.agentfield_run stays as an internal read; the desk needs none.
	var tree appwire.FactoryAgentFieldRun
	_ = json.Unmarshal(afCall(s, "factory.agentfield_run", map[string]any{"factory": st.Factory, "job": job}), &tree)
	if len(tree.Steps) != 4 || tree.Steps[3].Agent != chats.Steps[0].Agent || !tree.Steps[3].Steerable || len(tree.Steps[3].Children) != 2 {
		t.Fatalf("factory.agentfield_run is %+v", tree)
	}

	// The step chat's ordinary send is the steer.
	s.must("agents.send", map[string]any{"id": chats.Steps[0].Agent, "text": "check negative amounts too"}, nil)
	cp.mu.Lock()
	steers := append([]string(nil), cp.steers...)
	cp.mu.Unlock()
	if len(steers) != 1 || steers[0] != "e-i: check negative amounts too" {
		t.Fatalf("the send reached the run as %v", steers)
	}

	cp.mu.Lock()
	end := "2026-11-07T12:00:09Z"
	cp.tree.Status, cp.tree.Children[3].Status, cp.tree.Children[3].CompletedAt = "succeeded", "succeeded", &end
	cp.tree.Children[3].Children[1].Status, cp.tree.Children[3].Children[1].CompletedAt = "succeeded", &end
	cp.held["e-i"] = append(cp.held["e-i"], agentfield.RelayEntry{Seq: 5, Kind: "ended", Data: json.RawMessage(`{}`)})
	cp.held["e-v"] = append(cp.held["e-v"], agentfield.RelayEntry{Seq: 4, Kind: "ended", Data: json.RawMessage(`{}`)})
	cp.mu.Unlock()
	eventually(t, "the job is done", func() bool {
		var j struct{ State string }
		_ = json.Unmarshal(afCall(s, "factory.job", map[string]any{"factory": st.Factory, "job": job}), &j)
		return j.State == "done"
	})
	// A FINISHED RUN_ON JOB SAYS WHEN IT ENDED, as a native job does: the
	// run's own end.
	var finished struct {
		DoneAt time.Time `json:"done_at"`
	}
	_ = json.Unmarshal(afCall(s, "factory.job", map[string]any{"factory": st.Factory, "job": job}), &finished)
	if finished.DoneAt.IsZero() {
		t.Fatal("the finished job has no done_at")
	}
	msg := s.mustFail("agents.send", map[string]any{"id": chats.Steps[0].Agent, "text": "one more thing"}, appwire.CodeConflict)
	if !strings.Contains(msg, afSteerEnded) {
		t.Fatalf("a send to the ended step said %q", msg)
	}
}

func afCall(s *localSetup, method string, params any) json.RawMessage {
	s.t.Helper()
	raw, code, msg := s.call(method, params)
	if code != "" {
		s.t.Fatalf("%s: %s %s", method, code, msg)
	}
	return raw
}

// A held chat opens a step's conversation the moment the step is listed, and
// an opener that finds the record empty writes its own header. The live run
// caught the mirror then appending the node's header second, so the record
// opened twice. The mirror is minted with its header, so an opener finds it
// whole and the node's header is a repeat.
func TestAMirrorOpenedBeforeItsFirstLineHasOneHeader(t *testing.T) {
	appDayProfile(t)
	workspace := t.TempDir()
	st, it := afJob(t)
	r := &afRemote{items: st, floor: st, workspace: workspace}
	m := r.mirror(it, "exec-inv", "investigate", true)
	if m == nil {
		t.Fatal("no mirror was minted")
	}
	// What an opener does to an empty record.
	if info, err := os.Stat(m.file); err != nil || info.Size() == 0 {
		_ = os.WriteFile(m.file, []byte(`{"type":"session","version":1,"id":"opener"}`+"\n"), 0o600)
	}
	header := []byte(`{"type":"session","version":1,"id":"nodefolder","cwd":"/node/work","timestamp":"2026-11-07T12:00:00Z"}`)
	line := []byte(`{"type":"message","role":"user","content":"hello","timestamp":"2026-11-07T12:00:01Z"}`)
	if err := m.append([][]byte{header, line}, workspace); err != nil {
		t.Fatal(err)
	}
	kept := afRecordLines(t, m.file)
	headers := 0
	for _, l := range kept {
		if afIsHeader(l) {
			headers++
		}
	}
	if headers != 1 || len(kept) != 2 || !bytes.Equal(kept[1], line) {
		t.Fatalf("the conversation is\n%s\nwant one header and the line", bytes.Join(kept, []byte("\n")))
	}
}

// A REPLAY IS NEVER A SECOND COPY. After a control plane restart the node can
// resend a batch this factory already kept and acked, beside newer entries
// and out of order; the record keeps each (execution, seq) once, in order.
func TestAReplayedBatchIsKeptOnce(t *testing.T) {
	appDayProfile(t)
	workspace := t.TempDir()
	st, it := afJob(t)
	line := func(n int) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"type":"message","role":"assistant","content":"line %d","timestamp":"2026-11-07T12:00:00Z"}`, n))
	}
	cp := &afFakeCP{tree: afNode("exec-root", "handle_ticket", "running", "2026-11-07T12:00:00Z", "",
		afNode("exec-inv", "investigate", "running", "2026-11-07T12:00:01Z", ""))}
	cp.held = map[string][]agentfield.RelayEntry{"exec-inv": {
		{Seq: 1, Kind: "started", Data: json.RawMessage(`{"harness":"codeaf","steerable":true}`)},
		{Seq: 2, Kind: "line", Data: line(1)},
		{Seq: 3, Kind: "line", Data: line(2)},
	}}
	r := &afRemote{items: st, floor: st, workspace: workspace}
	r.sync(context.Background(), cp, it.ID, cp.tree)
	if n := cp.heldCount("exec-inv"); n != 0 {
		t.Fatalf("the first batch was not acked: %d held", n)
	}
	// The node's replay after a restart: 2 and 3 again, 4, a duplicate 4, out of order.
	cp.mu.Lock()
	cp.held["exec-inv"] = []agentfield.RelayEntry{{Seq: 4, Kind: "line", Data: line(3)}, {Seq: 3, Kind: "line", Data: line(2)},
		{Seq: 2, Kind: "line", Data: line(1)}, {Seq: 4, Kind: "line", Data: line(3)}}
	cp.mu.Unlock()
	again := &afRemote{items: st, floor: st, workspace: workspace}
	again.sync(context.Background(), cp, it.ID, cp.tree)
	got, _ := st.Get(it.ID)
	kept := afRecordLines(t, got.Stream.Phases[0].Chat)
	if len(kept) != 4 {
		t.Fatalf("the record has %d lines, want the header and three lines once each:\n%s", len(kept), bytes.Join(kept, []byte("\n")))
	}
	for i := 1; i <= 3; i++ {
		if !bytes.Equal(kept[i], line(i)) {
			t.Fatalf("line %d is %s", i, kept[i])
		}
	}
	if n := cp.heldCount("exec-inv"); n != 0 {
		t.Fatalf("the replay was not acked: %d held", n)
	}
}

// A RUN THAT ENDED IS READ TO ITS LAST LINE. The node relays a step's record
// beside the step, so the run can be over on the control plane while the
// step's last lines and its "ended" are still on their way; the live run left
// 29 of them held. They are kept and acked, and the control plane holds none.
func TestARunThatEndedIsReadToItsLastLine(t *testing.T) {
	appDayProfile(t)
	prev := afRemoteEvery
	afRemoteEvery = 10 * time.Millisecond
	t.Cleanup(func() { afRemoteEvery = prev })
	st, it := afJob(t)
	cp := &afFakeCP{tree: afNode("exec-root", "handle_ticket", "succeeded", "2026-11-07T12:00:00Z", "2026-11-07T12:00:09Z",
		afNode("e-d", "draft_reply", "succeeded", "2026-11-07T12:00:01Z", "2026-11-07T12:00:08Z"))}
	cp.relayLines("e-d", [][]byte{[]byte(`{"type":"message","role":"user","content":"draft it","timestamp":"2026-11-07T12:00:01Z"}`)}, false)
	r := &afRemote{items: st, floor: st, workspace: t.TempDir(), conn: func() (afClient, string, bool) { return cp, "workaf", true }}
	done := make(chan error, 1)
	go func() { done <- r.Run(context.Background(), it.ID) }()
	time.Sleep(60 * time.Millisecond)
	late := []byte(`{"type":"message","role":"assistant","content":"Dear customer,","timestamp":"2026-11-07T12:00:08Z"}`)
	cp.mu.Lock()
	cp.held["e-d"] = append(cp.held["e-d"], agentfield.RelayEntry{Seq: 3, Kind: "line", Data: late}, agentfield.RelayEntry{Seq: 4, Kind: "ended", Data: json.RawMessage(`{}`)})
	cp.mu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run was never read to its end")
	}
	if n := cp.heldCount("e-d"); n != 0 {
		t.Fatalf("the control plane still holds %d entries", n)
	}
	got, _ := st.Get(it.ID)
	kept := afRecordLines(t, got.Stream.Phases[0].Chat)
	if !bytes.Equal(kept[len(kept)-1], late) {
		t.Fatalf("the last line was not kept: %s", bytes.Join(kept, []byte("\n")))
	}
}

// A STEP CAN HOLD TWO HARNESS RUNS. The harness's schema retry starts a second
// codeaf in the same execution: the first run's "ended" and the retry's
// "started" arrive in one batch, and the retry brings its own session header.
// The live run closed the record on that first "ended" and stranded the
// retry's 33 entries. The record stays open while the execution runs, keeps
// one header, and is read to the retry's own end.
func TestAStepsSchemaRetryIsReadToItsOwnEnd(t *testing.T) {
	appDayProfile(t)
	prev := afRemoteEvery
	afRemoteEvery = 10 * time.Millisecond
	t.Cleanup(func() { afRemoteEvery = prev })
	st, it := afJob(t)
	cp := &afFakeCP{tree: afNode("exec-root", "handle_ticket", "running", "2026-11-07T12:00:00Z", "",
		afNode("e-i", "investigate", "running", "2026-11-07T12:00:01Z", ""))}
	header := []byte(`{"type":"session","version":1,"id":"node-run-1","cwd":"/node/clone","timestamp":"2026-11-07T12:00:01Z"}`)
	first := []byte(`{"type":"message","role":"user","content":"investigate","timestamp":"2026-11-07T12:00:01Z"}`)
	cp.relayLines("e-i", [][]byte{header, first}, true) // seqs 1..4, "ended" at 4
	retryHeader := []byte(`{"type":"session","version":1,"id":"node-run-2","cwd":"/node/clone","timestamp":"2026-11-07T12:00:05Z"}`)
	retry := []byte(`{"type":"message","role":"user","content":"PREVIOUS ATTEMPT FAILED","timestamp":"2026-11-07T12:00:05Z"}`)
	cp.mu.Lock()
	cp.held["e-i"] = append(cp.held["e-i"],
		agentfield.RelayEntry{Seq: 5, Kind: "started", Data: json.RawMessage(`{"harness":"codeaf","steerable":true}`)},
		agentfield.RelayEntry{Seq: 6, Kind: "line", Data: retryHeader},
		agentfield.RelayEntry{Seq: 7, Kind: "line", Data: retry})
	cp.mu.Unlock()
	r := &afRemote{items: st, floor: st, workspace: t.TempDir(), conn: func() (afClient, string, bool) { return cp, "workaf", true }}
	done := make(chan error, 1)
	go func() { done <- r.Run(context.Background(), it.ID) }()
	time.Sleep(80 * time.Millisecond)
	if r.drained("e-i") {
		t.Fatal("the record closed on the first run's end while the step still runs")
	}
	answer := []byte(`{"type":"message","role":"assistant","content":"the fix","timestamp":"2026-11-07T12:00:08Z"}`)
	end := "2026-11-07T12:00:09Z"
	cp.mu.Lock()
	cp.held["e-i"] = append(cp.held["e-i"], agentfield.RelayEntry{Seq: 8, Kind: "line", Data: answer}, agentfield.RelayEntry{Seq: 9, Kind: "ended", Data: json.RawMessage(`{}`)})
	cp.tree = afNode("exec-root", "handle_ticket", "succeeded", "2026-11-07T12:00:00Z", end,
		afNode("e-i", "investigate", "succeeded", "2026-11-07T12:00:01Z", end))
	cp.mu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run was never read to its end")
	}
	if n := cp.heldCount("e-i"); n != 0 {
		t.Fatalf("the control plane still holds %d entries", n)
	}
	got, _ := st.Get(it.ID)
	kept := afRecordLines(t, got.Stream.Phases[0].Chat)
	headers := 0
	for _, l := range kept {
		if bytes.Contains(l, []byte(`"type":"session"`)) {
			headers++
		}
	}
	if headers != 1 || !bytes.Equal(kept[len(kept)-1], answer) || !bytes.Contains(bytes.Join(kept, nil), retry) {
		t.Fatalf("the record is\n%s", bytes.Join(kept, []byte("\n")))
	}
}

func TestARunsStepNamesReadAsWords(t *testing.T) {
	for in, want := range map[string]string{
		"investigate_ncruces": "Investigate ncruces",
		"draft_reply":         "Draft reply",
		"draftReply":          "Draft reply",
		"search-docs":         "Search docs",
		"plan":                "Plan",
		"handle_ticket.v2":    "Handle ticket v2",
		"__":                  "__",
	} {
		if got := afLabel(in); got != want {
			t.Errorf("afLabel(%q) = %q, want %q", in, got, want)
		}
	}
	if got := afSharedWords([]string{"Investigate ncruces", "Investigate mattn", "Investigate modernc"}); got != "Investigate" {
		t.Errorf("shared %q", got)
	}
	if got := afSharedWords([]string{"Lint", "Test"}); got != "" {
		t.Errorf("unrelated steps share %q", got)
	}
}
