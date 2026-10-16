package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── the fixtures: the real wire server and the real backend over producers
// whose calls are counted. No model, no network. ───────────────────────────

// actionProducers are the three producers' doors, scripted. One value is shared
// by every backend a test builds, so a "restart" keeps counting the same calls.
type actionProducers struct {
	mu       sync.Mutex
	tasks    int // task rows admitted
	delegate int
	remember int
	briefs   []string
	next     uint64
	// taskErr and memErr fail BEFORE the effect; postErr and memPostErr fail
	// AFTER it (the row admitted, the line kept), which is the shape of a real
	// producer that reserves, registers or writes before some of its errors.
	taskErr    error
	postErr    error
	progErr    error
	memErr     error
	memPostErr error
	// hold, when set, blocks a task start until it is closed.
	hold      chan struct{}
	entered   chan struct{}
	kept      bool
	keptLines int // lines actually kept by Remember
}

type actionConv struct {
	*fakeConversation
	p *actionProducers
}

func (c *actionConv) admit(brief string) (uint64, error) {
	c.p.mu.Lock()
	c.p.briefs = append(c.p.briefs, brief)
	hold, entered := c.p.hold, c.p.entered
	c.p.mu.Unlock()
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if hold != nil {
		<-hold
	}
	c.p.mu.Lock()
	defer c.p.mu.Unlock()
	if c.p.taskErr != nil {
		return 0, c.p.taskErr
	}
	c.p.tasks++
	c.p.next++
	if c.p.postErr != nil {
		return 0, c.p.postErr
	}
	return c.p.next, nil
}

func (c *actionConv) StartTask(_ context.Context, brief string, solo bool) (uint64, string, string, error) {
	id, err := c.admit(brief)
	if solo {
		c.p.mu.Lock()
		c.p.briefs = append(c.p.briefs, "solo")
		c.p.mu.Unlock()
	}
	return id, "Title of " + brief, "", err
}

func (c *actionConv) StartTaskEffort(_ context.Context, brief string, solo bool, effort string) (uint64, string, string, error) {
	id, err := c.admit(brief + "|" + effort)
	return id, "Title", "a note", err
}

func (c *actionConv) Delegates() session.DelegateReport {
	return session.DelegateReport{Rows: []session.DelegateRow{
		{Name: "review", Description: "review", Lands: "text", Args: "[scope]", BriefOptional: true},
		{Name: "sec", Description: "audit", Lands: "tree"},
	}}
}

func (c *actionConv) StartDelegate(_ context.Context, name, brief string) (uint64, string, string, error) {
	c.p.mu.Lock()
	defer c.p.mu.Unlock()
	c.p.delegate++
	c.p.briefs = append(c.p.briefs, name+"|"+brief)
	if c.p.progErr != nil {
		return 0, "", "", c.p.progErr
	}
	c.p.next++
	return c.p.next, name + " run", "", nil
}

func (c *actionConv) Remembers() bool { return c.p.kept }

func (c *actionConv) Remember(text string) (string, error) {
	c.p.mu.Lock()
	defer c.p.mu.Unlock()
	c.p.remember++
	c.p.briefs = append(c.p.briefs, "remember|"+text)
	if c.p.memErr != nil {
		return "", c.p.memErr
	}
	c.p.keptLines++
	if c.p.memPostErr != nil {
		return "", c.p.memPostErr
	}
	return "remembered · " + text + " · for this project", nil
}

func (c *actionConv) RememberAlways(text string, everywhere bool) (string, error) {
	c.p.mu.Lock()
	defer c.p.mu.Unlock()
	c.p.remember++
	c.p.briefs = append(c.p.briefs, "always|"+text)
	return "remembered · " + text + " · everywhere", nil
}

func (p *actionProducers) counts() (tasks, delegate, remember int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tasks, p.delegate, p.remember
}

type actionHarness struct {
	t   *testing.T
	p   *actionProducers
	mu  sync.Mutex
	cvs map[string]*actionConv
	b   *appBackend
	w   *actionWire
}

func newActionHarness(t *testing.T) *actionHarness {
	t.Helper()
	wakeEnvironment(t, resolvedTempDir(t))
	h := &actionHarness{t: t, p: &actionProducers{kept: true}, cvs: map[string]*actionConv{}}
	h.restart()
	return h
}

// restart closes the backend and the wire and builds new ones over the same
// state folder, conversations and producers: a process that was lost.
func (h *actionHarness) restart() {
	h.t.Helper()
	if h.w != nil {
		h.w.close()
	}
	known := map[string]string{}
	if h.b != nil {
		h.b.locked(func() {
			for id, a := range h.b.agents {
				known[id] = a.transcript
			}
		})
		h.b.Close()
	}
	h.b = newAppBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		c := h.cvs[ask.Transcript]
		if c == nil {
			c = &actionConv{fakeConversation: &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}, p: h.p}
			h.cvs[ask.Transcript] = c
		}
		return &appConn{Conv: c, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	// A conversation never spoken in has no transcript to find it by; the new
	// process knows the ones the old one made, as a real one reads them off disk.
	for id, transcript := range known {
		if transcript != "" {
			h.b.locked(func() { h.b.agentLocked(id).transcript = transcript })
		}
	}
	b := h.b
	h.t.Cleanup(func() { b.Close() })
	h.w = newActionWire(h.t, h.b)
}

func (h *actionHarness) agent() string {
	h.t.Helper()
	a, err := mustCreate(h.t, h.b, appwire.CreateAsk{Workspace: h.t.TempDir()})
	if err != nil {
		h.t.Fatal(err)
	}
	return a.ID
}

// actionWire is a client of the real appwire.Server.
type actionWire struct {
	t       *testing.T
	in      *io.PipeWriter
	mu      sync.Mutex
	next    int
	waiting map[int]chan actionFrame
	hello   []string
	stop    sync.Once
}

type actionFrame struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *appwire.Error  `json:"error"`
}

func newActionWire(t *testing.T, b *appBackend) *actionWire {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	server := appwire.NewServer(outW)
	go func() {
		_ = server.Serve(context.Background(), inR, appwire.Hello{Version: "test"}, b)
		outW.Close()
	}()
	w := &actionWire{t: t, in: inW, waiting: map[int]chan actionFrame{}}
	scanner := bufio.NewScanner(outR)
	scanner.Buffer(make([]byte, 4<<20), 4<<20)
	if !scanner.Scan() {
		t.Fatal("no hello")
	}
	var hello struct {
		Data struct {
			Features []string `json:"features"`
		} `json:"data"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &hello); err != nil {
		t.Fatal(err)
	}
	w.hello = hello.Data.Features
	go func() {
		for scanner.Scan() {
			var f actionFrame
			if json.Unmarshal(scanner.Bytes(), &f) != nil || f.ID == 0 {
				continue
			}
			w.mu.Lock()
			ch := w.waiting[f.ID]
			delete(w.waiting, f.ID)
			w.mu.Unlock()
			if ch != nil {
				ch <- f
			}
		}
	}()
	t.Cleanup(w.close)
	return w
}

func (w *actionWire) close() { w.stop.Do(func() { w.in.Close() }) }

// call sends one request and waits for its answer.
func (w *actionWire) call(method string, params any) (json.RawMessage, *appwire.Error) {
	w.t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		w.t.Fatal(err)
	}
	ch := make(chan actionFrame, 1)
	w.mu.Lock()
	w.next++
	id := w.next
	w.waiting[id] = ch
	w.mu.Unlock()
	line := `{"id":` + actionItoa(id) + `,"method":"` + method + `","params":` + string(raw) + "}\n"
	if _, err := io.WriteString(w.in, line); err != nil {
		w.t.Fatal(err)
	}
	select {
	case f := <-ch:
		return f.Result, f.Error
	case <-time.After(30 * time.Second):
		w.t.Fatalf("%s: no answer", method)
		return nil, nil
	}
}

func actionItoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// act is session.act; a wire error is returned as it came.
func (w *actionWire) act(ask appwire.ActionAsk) (appwire.ActionReceipt, *appwire.Error) {
	w.t.Helper()
	raw, e := w.call("session.act", ask)
	if e != nil {
		return appwire.ActionReceipt{}, e
	}
	var r appwire.ActionReceipt
	if err := json.Unmarshal(raw, &r); err != nil {
		w.t.Fatal(err)
	}
	return r, nil
}

func (w *actionWire) get(agent, id string) (appwire.ActionReceipt, *appwire.Error) {
	w.t.Helper()
	raw, e := w.call("session.action.get", appwire.ActionGetParams{Agent: agent, ClientID: id})
	if e != nil {
		return appwire.ActionReceipt{}, e
	}
	var r appwire.ActionReceipt
	if err := json.Unmarshal(raw, &r); err != nil {
		w.t.Fatal(err)
	}
	return r, nil
}

func taskAsk(agent, id, brief string) appwire.ActionAsk {
	return appwire.ActionAsk{Action: appwire.ActionTaskStart, Agent: agent, ClientID: id, Task: &appwire.TaskStartInput{Brief: brief}}
}

func programAsk(agent, id, program, brief string) appwire.ActionAsk {
	return appwire.ActionAsk{Action: appwire.ActionProgramStart, Agent: agent, ClientID: id, Program: &appwire.ProgramStartInput{Program: program, Brief: brief}}
}

func memoryAsk(agent, id, text, scope string) appwire.ActionAsk {
	return appwire.ActionAsk{Action: appwire.ActionMemoryRemember, Agent: agent, ClientID: id, Memory: &appwire.MemoryRememberInput{Text: text, Scope: scope}}
}

func wantCode(t *testing.T, e *appwire.Error, code string) {
	t.Helper()
	if e == nil || e.Code != code {
		t.Fatalf("want a %s error, got %+v", code, e)
	}
}

func (h *actionHarness) claimFiles() []string {
	h.t.Helper()
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(h.b.state.where()), appActionClaimsName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		h.t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// ── the tests ───────────────────────────────────────────────────────────────

// THE ENGINE ANNOUNCES THE THREE METHODS AND SAYS WHAT A CONVERSATION CAN DO,
// the programs being the conversation engine's own rows.
func TestSessionActionsAreAnnouncedAndCapabilitiesAreTheEnginesOwn(t *testing.T) {
	h := newActionHarness(t)
	if !strings.Contains(strings.Join(h.w.hello, " "), appwire.FeatureSessionActions) {
		t.Fatalf("hello features = %v", h.w.hello)
	}
	agent := h.agent()
	raw, e := h.w.call("session.actions", appwire.ActionsParams{Agent: agent})
	if e != nil {
		t.Fatal(e)
	}
	var caps appwire.ActionCapabilities
	if err := json.Unmarshal(raw, &caps); err != nil {
		t.Fatal(err)
	}
	if len(caps.Actions) != 3 || caps.Actions[0].Action != appwire.ActionTaskStart ||
		caps.Actions[1].Action != appwire.ActionProgramStart || caps.Actions[2].Action != appwire.ActionMemoryRemember {
		t.Fatalf("capabilities: %+v", caps)
	}
	for _, c := range caps.Actions {
		if !c.Available || c.Why != "" {
			t.Errorf("%s: %+v", c.Action, c)
		}
	}
	if p := caps.Actions[1].Programs; len(p) != 2 || p[0].Name != "review" || !p[0].BriefOptional || p[1].BriefOptional {
		t.Errorf("programs are the conversation engine's rows: %+v", p)
	}
	if got := caps.Actions[2].Scopes; len(got) != 2 {
		t.Errorf("scopes: %v", got)
	}

	// A conversation that keeps no memory has the verb absent, with its reason.
	h.p.kept = false
	raw, e = h.w.call("session.actions", appwire.ActionsParams{Agent: agent})
	if e != nil {
		t.Fatal(e)
	}
	caps = appwire.ActionCapabilities{}
	_ = json.Unmarshal(raw, &caps)
	if m := caps.Actions[2]; m.Available || m.Why == "" || len(m.Scopes) != 0 {
		t.Errorf("memory with no store: %+v", m)
	}
	_, e = h.w.act(memoryAsk(agent, "m-off", "x", ""))
	wantCode(t, e, appwire.CodeBadParams)
	if len(h.claimFiles()) != 0 {
		t.Errorf("a refused shape left a claim: %v", h.claimFiles())
	}
	_, e = h.w.call("session.actions", appwire.ActionsParams{Agent: "no-such"})
	wantCode(t, e, appwire.CodeNotFound)
}

// A TASK START RETURNS THE PRODUCER'S IDS, IS CLAIMED BEFORE IT RUNS, AND A
// REPEAT IS ANSWERED FROM THE RECORD WITH THE PRODUCER NOT CALLED AGAIN. The
// record holds the digest and never the brief.
func TestTaskStartIsIdempotentAndKeepsNoWords(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	const brief = "  rebuild the docs site — a very private sentence  "
	r, e := h.w.act(taskAsk(agent, "t-1", brief))
	if e != nil {
		t.Fatal(e)
	}
	if r.State != appwire.ActionDone || r.Replayed || r.Task == nil || r.Task.ID != agent+":1" || r.Task.Number != 1 ||
		r.Task.Title != "Title of rebuild the docs site — a very private sentence" || r.Digest == "" {
		t.Fatalf("first receipt: %+v %+v", r, r.Task)
	}
	again, e := h.w.act(taskAsk(agent, "t-1", "rebuild the docs site — a very private sentence"))
	if e != nil {
		t.Fatal(e)
	}
	if !again.Replayed || again.State != appwire.ActionDone || again.Task == nil || again.Task.ID != r.Task.ID || again.Digest != r.Digest ||
		again.Task.Title != "" || again.Task.Number != 1 {
		t.Fatalf("replay (trim-equal words are the same request): %+v", again)
	}
	if tasks, _, _ := h.p.counts(); tasks != 1 {
		t.Fatalf("the producer ran %d times", tasks)
	}
	got, e := h.w.get(agent, "t-1")
	if e != nil || got.State != appwire.ActionDone || !got.Replayed || got.Task.ID != r.Task.ID {
		t.Fatalf("get: %+v %v", got, e)
	}
	files := h.claimFiles()
	if len(files) != 1 {
		t.Fatalf("claim files: %v", files)
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(h.b.state.where()), appActionClaimsName, files[0]))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private") || strings.Contains(files[0], "t-1") {
		t.Fatalf("the claim holds the person's words or the id is a path: %s %s", files[0], raw)
	}
}

// THE EFFORT IS FORWARDED TO THE EFFORT DOOR AND NOTHING ELSE CHANGES.
func TestTaskStartEffortAndSoloGoThroughTheProducersOwnDoors(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	ask := taskAsk(agent, "t-e", "do it")
	ask.Task.Effort = appwire.EffortBest
	r, e := h.w.act(ask)
	if e != nil || r.State != appwire.ActionDone || r.Task.Note != "a note" {
		t.Fatalf("%+v %v", r, e)
	}
	h.p.mu.Lock()
	got := h.p.briefs[len(h.p.briefs)-1]
	h.p.mu.Unlock()
	if got != "do it|best" {
		t.Fatalf("the effort door was handed %q", got)
	}
	solo := taskAsk(agent, "t-s", "alone")
	solo.Task.Solo = true
	r, e = h.w.act(solo)
	if e != nil || !r.Task.Solo {
		t.Fatalf("%+v %v", r, e)
	}
}

// EIGHT PARALLEL CALLS WITH ONE CLIENT_ID RUN THE PRODUCER ONCE. The others
// are answered in_progress (or from the record) and none is run.
func TestParallelDuplicatesRunTheProducerOnce(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	h.p.hold = make(chan struct{})
	h.p.entered = make(chan struct{}, 1)
	var wg sync.WaitGroup
	results := make([]appwire.ActionReceipt, 8)
	errs := make([]*appwire.Error, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = h.w.act(taskAsk(agent, "t-par", "once"))
		}()
	}
	<-h.p.entered
	// While it runs, a read says so, and another id on the agent is busy.
	got, e := h.w.get(agent, "t-par")
	if e != nil || got.State != appwire.ActionInProgress {
		t.Fatalf("a read while it runs: %+v %v", got, e)
	}
	_, e = h.w.act(taskAsk(agent, "t-other", "another"))
	wantCode(t, e, appwire.CodeBusy)
	close(h.p.hold)
	wg.Wait()
	done, progress := 0, 0
	for i, r := range results {
		if errs[i] != nil {
			t.Fatalf("call %d: %+v", i, errs[i])
		}
		switch r.State {
		case appwire.ActionDone:
			done++
		case appwire.ActionInProgress:
			progress++
		default:
			t.Fatalf("call %d: %+v", i, r)
		}
	}
	if done < 1 || done+progress != 8 {
		t.Fatalf("done %d in_progress %d", done, progress)
	}
	if tasks, _, _ := h.p.counts(); tasks != 1 {
		t.Fatalf("the producer ran %d times", tasks)
	}
	final, e := h.w.get(agent, "t-par")
	if e != nil || final.State != appwire.ActionDone {
		t.Fatalf("after: %+v %v", final, e)
	}
}

// A LOST PROCESS: the record survives a restart and replays; an attempt that
// was claimed and never settled is uncertain for ever and is never run.
func TestRestartReplaysAndAnUnsettledClaimIsNeverRunAgain(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	first, e := h.w.act(taskAsk(agent, "t-r", "survive"))
	if e != nil {
		t.Fatal(e)
	}
	// A claim the previous process wrote at intent and died on.
	lost := appActionClaim{V: appActionClaimV, ClientID: "t-lost", Action: appwire.ActionTaskStart, Agent: agent,
		Digest: appActionDigest(taskAsk(agent, "t-lost", "never ran twice")), State: appActionIntent, At: time.Now().UTC()}
	if made, err := h.b.actionClaims().create(lost); err != nil || !made {
		t.Fatalf("seed: %v %v", made, err)
	}
	h.restart()
	again, e := h.w.act(taskAsk(agent, "t-r", "survive"))
	if e != nil || !again.Replayed || again.State != appwire.ActionDone || again.Task.ID != first.Task.ID {
		t.Fatalf("replay after restart: %+v %v", again, e)
	}
	r, e := h.w.act(taskAsk(agent, "t-lost", "never ran twice"))
	if e != nil || r.State != appwire.ActionUncertain || !r.Replayed || r.Note == "" {
		t.Fatalf("an unsettled claim: %+v %v", r, e)
	}
	got, e := h.w.get(agent, "t-lost")
	if e != nil || got.State != appwire.ActionUncertain {
		t.Fatalf("get of an unsettled claim: %+v %v", got, e)
	}
	if tasks, _, _ := h.p.counts(); tasks != 1 {
		t.Fatalf("the producer ran %d times in all", tasks)
	}
}

// UNKNOWN RECEIPT, AND A RECORD THAT CANNOT BE READ: not_found says only that
// this home has none; damage is uncertain and is never taken for "new".
func TestUnknownAndUnreadableReceipts(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	_, e := h.w.get(agent, "never")
	wantCode(t, e, appwire.CodeNotFound)
	if !strings.Contains(e.Message, "does not prove") {
		t.Errorf("not_found must say it is no proof: %q", e.Message)
	}
	if _, e := h.w.act(taskAsk(agent, "t-d", "x")); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(filepath.Dir(h.b.state.where()), appActionClaimsName, appClaimName("t-d"))
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, e = h.w.get(agent, "t-d")
	wantCode(t, e, appwire.CodeUncertain)
	_, e = h.w.act(taskAsk(agent, "t-d", "x"))
	wantCode(t, e, appwire.CodeUncertain)
	if tasks, _, _ := h.p.counts(); tasks != 1 {
		t.Fatalf("a damaged record ran the producer again: %d", tasks)
	}
	// Not a plain file: refused, not followed.
	other := filepath.Join(filepath.Dir(path), appClaimName("t-link"))
	if err := os.Symlink(path, other); err != nil {
		t.Skip("no symlinks")
	}
	_, e = h.w.act(taskAsk(agent, "t-link", "x"))
	wantCode(t, e, appwire.CodeUncertain)
}

// THE OWNER IS THE CONVERSATION: another agent cannot use, read or replay a
// client_id, and cannot learn what it was for.
func TestForeignOwnerIsAConflict(t *testing.T) {
	h := newActionHarness(t)
	one, two := h.agent(), h.agent()
	if _, e := h.w.act(taskAsk(one, "t-o", "mine")); e != nil {
		t.Fatal(e)
	}
	_, e := h.w.act(taskAsk(two, "t-o", "mine"))
	wantCode(t, e, appwire.CodeConflict)
	_, e = h.w.get(two, "t-o")
	wantCode(t, e, appwire.CodeConflict)
	if tasks, _, _ := h.p.counts(); tasks != 1 {
		t.Fatalf("producer ran %d times", tasks)
	}
}

// THE SAME CLIENT_ID WITH OTHER WORDS, ANOTHER ACTION OR OTHER OPTIONS IS A
// CONFLICT AND THE PRODUCER IS NOT CALLED.
func TestSameClientDifferentPayloadIsAConflict(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	if _, e := h.w.act(taskAsk(agent, "t-c", "one")); e != nil {
		t.Fatal(e)
	}
	_, e := h.w.act(taskAsk(agent, "t-c", "two"))
	wantCode(t, e, appwire.CodeConflict)
	solo := taskAsk(agent, "t-c", "one")
	solo.Task.Solo = true
	_, e = h.w.act(solo)
	wantCode(t, e, appwire.CodeConflict)
	_, e = h.w.act(memoryAsk(agent, "t-c", "one", ""))
	wantCode(t, e, appwire.CodeConflict)
	if tasks, _, rem := h.p.counts(); tasks != 1 || rem != 0 {
		t.Fatalf("producers ran: tasks %d remember %d", tasks, rem)
	}
}

// A MALFORMED ASK IS bad_params BEFORE ANY CLAIM OR PRODUCER.
func TestMalformedAsksNeverClaimOrRun(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	tooLong := strings.Repeat("a", appwire.ActionBriefMax+1)
	bad := map[string]appwire.ActionAsk{
		"no client id":         taskAsk(agent, "", "x"),
		"padded client id":     taskAsk(agent, " x", "x"),
		"long client id":       taskAsk(agent, strings.Repeat("c", 129), "x"),
		"no agent":             taskAsk("", "c", "x"),
		"empty brief":          taskAsk(agent, "c", "   "),
		"long brief":           taskAsk(agent, "c", tooLong),
		"control in brief":     taskAsk(agent, "c", "a\x00b"),
		"unknown action":       {Action: "task.delete", Agent: agent, ClientID: "c", Task: &appwire.TaskStartInput{Brief: "x"}},
		"no input":             {Action: appwire.ActionTaskStart, Agent: agent, ClientID: "c"},
		"wrong input":          {Action: appwire.ActionTaskStart, Agent: agent, ClientID: "c", Memory: &appwire.MemoryRememberInput{Text: "x"}},
		"two inputs":           {Action: appwire.ActionTaskStart, Agent: agent, ClientID: "c", Task: &appwire.TaskStartInput{Brief: "x"}, Memory: &appwire.MemoryRememberInput{Text: "x"}},
		"bad effort":           {Action: appwire.ActionTaskStart, Agent: agent, ClientID: "c", Task: &appwire.TaskStartInput{Brief: "x", Effort: "max"}},
		"program as a path":    programAsk(agent, "c", "../bin/sh", "x"),
		"program unknown":      programAsk(agent, "c", "nope", "x"),
		"program needs brief":  programAsk(agent, "c", "sec", ""),
		"program brief long":   programAsk(agent, "c", "review", tooLong),
		"memory empty":         memoryAsk(agent, "c", " ", ""),
		"memory long":          memoryAsk(agent, "c", strings.Repeat("m", appwire.ActionTextMax+1), ""),
		"memory scope":         memoryAsk(agent, "c", "x", "world"),
		"memory control":       memoryAsk(agent, "c", "x\x07", ""),
		"agent with a slash":   taskAsk("a\x00b", "c", "x"),
		"agent control (path)": taskAsk("../../x\n", "c", "x"),
	}
	for name, ask := range bad {
		if _, e := h.w.act(ask); e == nil || e.Code != appwire.CodeBadParams {
			t.Errorf("%s: want bad_params, got %+v", name, e)
		}
	}
	_, e := h.w.act(taskAsk("no-such-conversation", "c", "x"))
	wantCode(t, e, appwire.CodeNotFound)
	if files := h.claimFiles(); len(files) != 0 {
		t.Errorf("malformed asks left claims: %v", files)
	}
	if tasks, d, r := h.p.counts(); tasks+d+r != 0 {
		t.Errorf("malformed asks ran producers: %d %d %d", tasks, d, r)
	}
	// A body that is not an ask at all.
	_, e = h.w.call("session.act", map[string]any{"action": "task.start", "agent": agent, "client_id": "c", "task": map[string]any{"brief": "x", "bypass_budget": true}})
	wantCode(t, e, appwire.CodeBadParams)
}

// A PRODUCER'S ERROR — A BUDGET, A BLOCKED PROVIDER, ANYTHING — IS UNCERTAIN,
// NEVER A REFUSAL: no producer proves by type that its error came before any
// effect. The receipt keeps its words (secrets taken out), the client_id is
// spent for ever, and a new one asks again.
func TestProducerErrorsAreUncertainNeverRefused(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	for _, c := range []struct{ id, words, want string }{
		{"t-b", "today's spending cap is reached; raise it or wait until tomorrow", "cap is reached"},
		{"t-p", "the provider is blocked: key sk-or-v1-abcdefghijklmnopqrstuvwxyz0123456789abcdef rejected", "blocked"},
	} {
		h.p.mu.Lock()
		h.p.taskErr = errors.New(c.words)
		h.p.mu.Unlock()
		r, e := h.w.act(taskAsk(agent, c.id, "go"))
		if e != nil || r.State != appwire.ActionUncertain || r.ProducerError == nil || !strings.Contains(r.ProducerError.Message, c.want) ||
			r.Task != nil || r.Replayed || r.Note == "" {
			t.Fatalf("%s: %+v %v", c.id, r, e)
		}
		if strings.Contains(r.ProducerError.Message, "sk-or-v1-abcdefghijklmnopqrstuvwxyz0123456789abcdef") {
			t.Errorf("the error carries a secret: %q", r.ProducerError.Message)
		}
		calls := len(h.p.briefs)
		again, e := h.w.act(taskAsk(agent, c.id, "go"))
		if e != nil || again.State != appwire.ActionUncertain || !again.Replayed || again.ProducerError == nil ||
			again.ProducerError.Message != r.ProducerError.Message || len(h.p.briefs) != calls {
			t.Fatalf("%s replay: %+v %v", c.id, again, e)
		}
	}
	h.restart()
	got, e := h.w.get(agent, "t-b")
	if e != nil || got.State != appwire.ActionUncertain || got.ProducerError == nil {
		t.Fatalf("an error after restart: %+v %v", got, e)
	}
	// Cleared, the spent client_id still never runs; a NEW one does.
	h.p.mu.Lock()
	h.p.taskErr = nil
	h.p.mu.Unlock()
	if r, e := h.w.act(taskAsk(agent, "t-b", "go")); e != nil || r.State != appwire.ActionUncertain || !r.Replayed {
		t.Fatalf("the spent client_id: %+v %v", r, e)
	}
	if tasks, _, _ := h.p.counts(); tasks != 0 {
		t.Fatalf("the spent client_id ran: %d", tasks)
	}
	r, e := h.w.act(taskAsk(agent, "t-b2", "go"))
	if e != nil || r.State != appwire.ActionDone {
		t.Fatalf("%+v %v", r, e)
	}
}

// THE BLOCKER'S REGRESSION: a producer that HAS HAD ITS EFFECT and then answers
// an error. The old gateway called this refused — "nothing happened" — after a
// task row was admitted. It is uncertain, the effect happened once, and no
// repeat, restart or reconciliation read runs it again or calls it not sent.
func TestAnErrorAfterTheEffectIsUncertainAndRunsOnceAcrossRestart(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	h.p.mu.Lock()
	h.p.postErr = errors.New("the task was admitted but its first worker could not be registered")
	h.p.memPostErr = errors.New("the line was written but the block could not be refreshed")
	h.p.mu.Unlock()

	task, e := h.w.act(taskAsk(agent, "t-post", "ship it"))
	if e != nil || task.State != appwire.ActionUncertain || task.ProducerError == nil || task.Task != nil {
		t.Fatalf("a task error after the effect: %+v %v", task, e)
	}
	mem, e := h.w.act(memoryAsk(agent, "m-post", "we ship on Tuesdays", ""))
	if e != nil || mem.State != appwire.ActionUncertain || mem.ProducerError == nil || mem.Memory != nil {
		t.Fatalf("a memory error after the effect: %+v %v", mem, e)
	}
	effects := func() (int, int) {
		h.p.mu.Lock()
		defer h.p.mu.Unlock()
		return h.p.tasks, h.p.keptLines
	}
	if tasks, lines := effects(); tasks != 1 || lines != 1 {
		t.Fatalf("effects %d tasks, %d lines; want 1 and 1", tasks, lines)
	}
	// The producer would now succeed; it must not be asked.
	h.p.mu.Lock()
	h.p.postErr, h.p.memPostErr = nil, nil
	h.p.mu.Unlock()
	for round := 0; round < 2; round++ {
		for _, ask := range []appwire.ActionAsk{taskAsk(agent, "t-post", "ship it"), memoryAsk(agent, "m-post", "we ship on Tuesdays", "")} {
			r, e := h.w.act(ask)
			if e != nil || r.State != appwire.ActionUncertain || !r.Replayed || r.ProducerError == nil {
				t.Fatalf("round %d repeat of %s: %+v %v", round, ask.ClientID, r, e)
			}
			got, e := h.w.get(agent, ask.ClientID)
			if e != nil || got.State != appwire.ActionUncertain || got.ProducerError == nil {
				t.Fatalf("round %d read of %s: %+v %v", round, ask.ClientID, got, e)
			}
		}
		h.restart()
	}
	if tasks, lines := effects(); tasks != 1 || lines != 1 {
		t.Fatalf("after repeats and restarts: %d tasks, %d lines; want 1 and 1", tasks, lines)
	}
	if _, _, rem := h.p.counts(); rem != 1 {
		t.Fatalf("remember was called %d times", rem)
	}
}

// A REFUSAL THE GATEWAY CAN PROVE IS MADE BEFORE ANYTHING IS CLAIMED OR CALLED,
// and so leaves nothing: a conversation that keeps no memory, a program the
// engine does not carry, a program that needs a brief. The same client_id, once
// the ask can run, runs once.
func TestABenignRefusalBeforeDispatchLeavesNothing(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	h.p.mu.Lock()
	h.p.kept = false
	h.p.mu.Unlock()
	for name, ask := range map[string]appwire.ActionAsk{
		"memory off":         memoryAsk(agent, "pre-m", "keep this", ""),
		"unknown program":    programAsk(agent, "pre-p", "nope", "x"),
		"program wants word": programAsk(agent, "pre-q", "sec", " "),
	} {
		_, e := h.w.act(ask)
		wantCode(t, e, appwire.CodeBadParams)
		if _, e := h.w.get(agent, ask.ClientID); e == nil || e.Code != appwire.CodeNotFound {
			t.Fatalf("%s: a pre-dispatch refusal left a record: %+v", name, e)
		}
	}
	if files := h.claimFiles(); len(files) != 0 {
		t.Fatalf("pre-dispatch refusals left claims: %v", files)
	}
	if tasks, d, r := h.p.counts(); tasks+d+r != 0 {
		t.Fatalf("pre-dispatch refusals called producers: %d %d %d", tasks, d, r)
	}
	h.p.mu.Lock()
	h.p.kept = true
	h.p.mu.Unlock()
	for _, ask := range []appwire.ActionAsk{memoryAsk(agent, "pre-m", "keep this", ""), programAsk(agent, "pre-p", "sec", "x")} {
		for i := 0; i < 2; i++ {
			r, e := h.w.act(ask)
			if e != nil || r.State != appwire.ActionDone || r.Replayed != (i == 1) {
				t.Fatalf("%s try %d: %+v %v", ask.ClientID, i, r, e)
			}
		}
	}
	if _, d, r := h.p.counts(); d != 1 || r != 1 {
		t.Fatalf("producers ran %d delegate, %d remember; want 1 and 1", d, r)
	}
}

// NO ANSWER IS NOT A REFUSAL: the attempt is uncertain, recorded as such, and
// run by nobody again — across a restart too.
func TestAnUnansweredProducerIsUncertainAndNeverRetried(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	h.p.taskErr = session.ErrSendUnanswered
	r, e := h.w.act(taskAsk(agent, "t-u", "maybe"))
	if e != nil || r.State != appwire.ActionUncertain || r.Note == "" || r.Task != nil || r.ProducerError != nil {
		t.Fatalf("%+v %v", r, e)
	}
	h.p.mu.Lock()
	h.p.taskErr = nil
	h.p.mu.Unlock()
	again, e := h.w.act(taskAsk(agent, "t-u", "maybe"))
	if e != nil || again.State != appwire.ActionUncertain || !again.Replayed {
		t.Fatalf("%+v %v", again, e)
	}
	h.restart()
	after, e := h.w.act(taskAsk(agent, "t-u", "maybe"))
	if e != nil || after.State != appwire.ActionUncertain || !after.Replayed {
		t.Fatalf("after restart: %+v %v", after, e)
	}
	if len(h.p.briefs) != 1 {
		t.Fatalf("the producer was called %d times", len(h.p.briefs))
	}
}

// IF THE CLAIM CANNOT BE WRITTEN THE PRODUCER IS NOT CALLED, and the engine
// says so with proof: `engine`, "did not start it", no record, and the same
// client_id runs once when the folder can be written again. The folder here
// is READABLE — the lookup before the claim succeeds and finds nothing — so it
// is the create that fails, not the read.
func TestAClaimThatCannotBeWrittenStopsTheProducer(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into a folder whatever its mode says")
	}
	h := newActionHarness(t)
	agent := h.agent()
	dir := filepath.Join(filepath.Dir(h.b.state.where()), appActionClaimsName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	_, e := h.w.act(taskAsk(agent, "t-w", "x"))
	wantCode(t, e, appwire.CodeEngine)
	if !strings.Contains(e.Message, "did not start it") {
		t.Fatalf("the refusal must say nothing started: %q", e.Message)
	}
	if len(h.p.briefs) != 0 {
		t.Fatalf("the producer ran without a claim: %v", h.p.briefs)
	}
	_, e = h.w.get(agent, "t-w")
	wantCode(t, e, appwire.CodeNotFound)
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		r, e := h.w.act(taskAsk(agent, "t-w", "x"))
		if e != nil || r.State != appwire.ActionDone || r.Replayed != (i == 1) {
			t.Fatalf("try %d once writable: %+v %v", i, r, e)
		}
	}
	if tasks, _, _ := h.p.counts(); tasks != 1 {
		t.Fatalf("the producer ran %d times", tasks)
	}
}

// A CLAIMS FOLDER THAT CANNOT EVEN BE READ IS UNCERTAIN, NOT "NEW": a file where
// the folder should be fails the lookup, and the producer is not called.
func TestAClaimsFolderThatCannotBeReadIsUncertain(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	dir := filepath.Join(filepath.Dir(h.b.state.where()), appActionClaimsName)
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("a file where the folder should be"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, e := h.w.act(taskAsk(agent, "t-w", "x"))
	wantCode(t, e, appwire.CodeUncertain)
	_, e = h.w.get(agent, "t-w")
	wantCode(t, e, appwire.CodeUncertain)
	if len(h.p.briefs) != 0 {
		t.Fatalf("the producer ran without a claim: %v", h.p.briefs)
	}
}

// PROGRAMS ARE NAMES IN THE ENGINE'S CATALOGUE; the producer gets the trimmed
// brief and a bare program with a default brief is allowed.
func TestProgramStartUsesTheCatalogueAndTheProducer(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	r, e := h.w.act(programAsk(agent, "p-1", "sec", "  audit the diff  "))
	if e != nil || r.State != appwire.ActionDone || r.Task.Program != "sec" || r.Task.ID != agent+":1" {
		t.Fatalf("%+v %v", r, e)
	}
	bare, e := h.w.act(programAsk(agent, "p-2", "review", ""))
	if e != nil || bare.State != appwire.ActionDone {
		t.Fatalf("a program with a default brief runs bare: %+v %v", bare, e)
	}
	h.p.mu.Lock()
	got := append([]string(nil), h.p.briefs...)
	h.p.mu.Unlock()
	if len(got) != 2 || got[0] != "sec|audit the diff" || got[1] != "review|" {
		t.Fatalf("producer saw %v", got)
	}
	// The producer's error (here: not in a task, no run road, home) is its
	// own words, and uncertain: the gateway cannot know it came first.
	h.p.mu.Lock()
	h.p.progErr = errors.New("a task cannot hand its work to sec; only the conversation can")
	h.p.mu.Unlock()
	failed, e := h.w.act(programAsk(agent, "p-3", "sec", "x"))
	if e != nil || failed.State != appwire.ActionUncertain || failed.ProducerError == nil || !strings.Contains(failed.ProducerError.Message, "only the conversation can") {
		t.Fatalf("%+v %v", failed, e)
	}
}

// REMEMBER KEEPS THE AUTHORED TEXT THROUGH THE PRODUCER AND RETURNS ITS RECEIPT.
func TestMemoryRememberKeepsAuthoredTextThroughTheProducer(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	r, e := h.w.act(memoryAsk(agent, "m-1", "  we deploy on Fridays  ", ""))
	if e != nil || r.State != appwire.ActionDone || r.Memory == nil || r.Memory.Scope != appwire.ScopeProject ||
		r.Memory.Receipt != "remembered · we deploy on Fridays · for this project" || r.Task != nil {
		t.Fatalf("%+v %v", r, e)
	}
	// "" and "project" are one request.
	same, e := h.w.act(memoryAsk(agent, "m-1", "we deploy on Fridays", appwire.ScopeProject))
	if e != nil || !same.Replayed {
		t.Fatalf("%+v %v", same, e)
	}
	every, e := h.w.act(memoryAsk(agent, "m-2", "always use tabs", appwire.ScopeEverywhere))
	if e != nil || every.Memory.Scope != appwire.ScopeEverywhere || !strings.Contains(every.Memory.Receipt, "everywhere") {
		t.Fatalf("%+v %v", every, e)
	}
	if _, _, rem := h.p.counts(); rem != 2 {
		t.Fatalf("remember ran %d times", rem)
	}
	h.p.mu.Lock()
	h.p.memErr = errors.New("memory is full")
	h.p.mu.Unlock()
	full, e := h.w.act(memoryAsk(agent, "m-3", "one more", ""))
	if e != nil || full.State != appwire.ActionUncertain || full.ProducerError == nil || full.ProducerError.Message != "memory is full" {
		t.Fatalf("%+v %v", full, e)
	}
}

// ADMISSION IS HELD UNTIL THE PRODUCER RETURNS: closing the engine does not free
// the conversation's slot while the mutation is still running.
func TestAdmissionOutlivesCloseWhileTheProducerRuns(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	h.p.hold = make(chan struct{})
	h.p.entered = make(chan struct{}, 1)
	var done atomic.Bool
	go func() {
		_, _ = h.b.SessionAct(taskAsk(agent, "t-h", "slow"))
		done.Store(true)
	}()
	<-h.p.entered
	go h.b.Close()
	time.Sleep(50 * time.Millisecond)
	if _, held := h.b.actions.flight(appClaimName("t-h")); !held || done.Load() {
		t.Fatal("the slot was released while the producer was still running")
	}
	if _, err := h.b.SessionAct(taskAsk(agent, "t-h2", "next")); err == nil {
		t.Fatal("a new action was admitted on a closing, busy engine")
	}
	close(h.p.hold)
	deadline := time.Now().Add(5 * time.Second)
	for !done.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !done.Load() {
		t.Fatal("the action never returned")
	}
	if _, held := h.b.actions.flight(appClaimName("t-h")); held {
		t.Fatal("the slot was not released after the producer returned")
	}
}

// AN ENGINE THAT IS NOT A SessionActioner REFUSES unknown_method; hello omits it.
func TestWithoutTheEngineHalfTheMethodsAreUnknown(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	server := appwire.NewServer(outW)
	go func() {
		_ = server.Serve(context.Background(), inR, appwire.Hello{Version: "test"}, plainEngine{})
		outW.Close()
	}()
	defer inW.Close()
	scanner := bufio.NewScanner(outR)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	scanner.Scan()
	if strings.Contains(scanner.Text(), appwire.FeatureSessionActions) {
		t.Fatalf("hello announced it: %s", scanner.Text())
	}
	for i, m := range []string{"session.actions", "session.act", "session.action.get"} {
		io.WriteString(inW, `{"id":`+actionItoa(i+1)+`,"method":"`+m+`","params":{"agent":"a","client_id":"c","action":"task.start","task":{"brief":"x"}}}`+"\n")
		for scanner.Scan() {
			var f actionFrame
			if json.Unmarshal(scanner.Bytes(), &f) == nil && f.ID == i+1 {
				wantCode(t, f.Error, appwire.CodeUnknownMethod)
				break
			}
		}
	}
}

type plainEngine struct{ appwire.Engine }

func (plainEngine) Close() error { return nil }
