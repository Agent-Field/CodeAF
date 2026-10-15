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
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// THE COMMAND ITSELF, ON ITS OWN PIPES. The hello is the first line and names
// the home the flag chose, ping answers, crews come off the teams file with
// their members named by agent id, an unknown method is refused, and the
// process leaves when its input ends.
func TestAppCommandAnswersHelloPingCrewsAndUnknownMethod(t *testing.T) {
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "unused"))
	t.Setenv(config.ProfileDirEnv, filepath.Join(root, "profile"))
	member := filepath.Join(root, "state", "v3", "places", "proj", "0123abcd", "transcript.jsonl")
	manager := filepath.Join(root, "state", "v3", "places", "proj", "89efcdab", "transcript.jsonl")
	if err := teamstore.Save(filepath.Join(root, "profile"), []teamstore.Team{{
		ID: "0a0a0a0a0a0a", Name: "harbor", Manager: manager,
		Members: []teamstore.Member{{Key: manager, File: manager, Handle: "boss"}, {Key: member, File: member, Handle: "web"}},
	}}); err != nil {
		t.Fatal(err)
	}

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = inR, outW
	t.Cleanup(func() { os.Stdin, os.Stdout = oldIn, oldOut })
	state := filepath.Join(root, "state")
	done := make(chan error, 1)
	go func() {
		done <- runApp([]string{"--home", state})
		outW.Close()
	}()
	lines := bufio.NewScanner(outR)
	next := func() map[string]any {
		t.Helper()
		if !lines.Scan() {
			t.Fatalf("the app's output ended: %v", lines.Err())
		}
		var line map[string]any
		if err := json.Unmarshal(lines.Bytes(), &line); err != nil {
			t.Fatalf("not a JSON line: %q", lines.Text())
		}
		return line
	}
	send := func(line string) {
		t.Helper()
		if _, err := io.WriteString(inW, line+"\n"); err != nil {
			t.Fatal(err)
		}
	}

	hello := next()
	data, _ := hello["data"].(map[string]any)
	if hello["event"] != "hello" || hello["seq"] != float64(1) || data["contract"] != float64(appwire.Contract) || data["home"] != state || data["pid"] != float64(os.Getpid()) {
		t.Fatalf("hello = %v", hello)
	}

	send(`{"id":1,"method":"ping"}`)
	if got := next(); got["id"] != float64(1) || got["result"] == nil {
		t.Fatalf("ping = %v", got)
	}

	send(`{"id":2,"method":"crews.list"}`)
	got := next()
	crews, _ := got["result"].(map[string]any)["crews"].([]any)
	if got["id"] != float64(2) || len(crews) != 1 {
		t.Fatalf("crews.list = %v", got)
	}
	crew := crews[0].(map[string]any)
	members := crew["members"].([]any)
	if crew["name"] != "harbor" || crew["manager"] != "89efcdab" || len(members) != 2 {
		t.Fatalf("crew = %v", crew)
	}
	if first := members[1].(map[string]any); first["agent"] != "0123abcd" || first["handle"] != "web" {
		t.Fatalf("member = %v", first)
	}

	send(`{"id":3,"method":"agents.teleport","params":{}}`)
	got = next()
	if got["id"] != float64(3) || got["error"].(map[string]any)["code"] != appwire.CodeUnknownMethod {
		t.Fatalf("unknown method = %v", got)
	}

	// Listing reads the disk and opens nothing: an empty home lists nothing.
	send(`{"id":4,"method":"agents.list"}`)
	got = next()
	if list, ok := got["result"].(map[string]any)["agents"].([]any); !ok || len(list) != 0 {
		t.Fatalf("agents.list = %v", got)
	}

	inW.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the app left with %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the app did not leave when its input ended")
	}
}

// fakeConversation plays a scripted turn and counts what is asked of it.
type fakeConversation struct {
	mu          sync.Mutex
	script      []session.Event
	hold        chan struct{}
	submits     int
	interrupted int
	needs       bool
	tasks       chan session.Event
	questions   chan session.Event
	endLane     func()
	answers     []session.Answer
	// onAnswer, when set, is what the engine does with an answer.
	onAnswer func(session.Answer)
}

func (f *fakeConversation) WatchQuestions() (<-chan session.Event, func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	lane := make(chan session.Event, 8)
	var once sync.Once
	end := func() { once.Do(func() { close(lane) }) }
	f.questions, f.endLane = lane, end
	return lane, end
}

func (f *fakeConversation) ResolveQuestion(answer session.Answer) error {
	f.mu.Lock()
	f.answers = append(f.answers, answer)
	on := f.onAnswer
	f.mu.Unlock()
	if on != nil {
		on(answer)
	}
	return nil
}

// raise puts one event on the conversation's question lane.
func (f *fakeConversation) raise(event session.Event) {
	f.mu.Lock()
	lane := f.questions
	f.mu.Unlock()
	lane <- event
}

func (f *fakeConversation) Submit(ctx context.Context, text string) (<-chan session.Event, error) {
	f.mu.Lock()
	f.submits++
	script, hold := f.script, f.hold
	f.mu.Unlock()
	events := make(chan session.Event)
	go func() {
		defer close(events)
		for _, event := range script {
			events <- event
		}
		if hold != nil {
			<-hold
		}
	}()
	return events, nil
}
func (f *fakeConversation) Interrupt()        { f.mu.Lock(); f.interrupted++; f.mu.Unlock() }
func (f *fakeConversation) Model() string     { return "deepseek/deepseek-v4-flash" }
func (f *fakeConversation) Title() string     { return "" }
func (f *fakeConversation) NeedsPerson() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.needs }
func (f *fakeConversation) WatchTaskUpdates() (<-chan session.Event, func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks = make(chan session.Event, 4)
	lane := f.tasks
	var once sync.Once
	return lane, func() { once.Do(func() { close(lane) }) }
}

// appRecorder is an Emitter a test can wait on.
type appRecorder struct {
	mu     sync.Mutex
	events []string
	data   []any
	more   chan struct{}
}

func newAppRecorder() *appRecorder { return &appRecorder{more: make(chan struct{}, 1)} }

func (r *appRecorder) Emit(event string, data any) {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.data = append(r.data, data)
	r.mu.Unlock()
	select {
	case r.more <- struct{}{}:
	default:
	}
}

// waitFor waits until an event satisfies match and answers its index.
func (r *appRecorder) waitFor(t *testing.T, within time.Duration, match func(string, any) bool) int {
	t.Helper()
	deadline := time.After(within)
	seen := 0
	for {
		r.mu.Lock()
		for ; seen < len(r.events); seen++ {
			if match(r.events[seen], r.data[seen]) {
				r.mu.Unlock()
				return seen
			}
		}
		r.mu.Unlock()
		select {
		case <-r.more:
		case <-deadline:
			r.mu.Lock()
			defer r.mu.Unlock()
			t.Fatalf("no matching event; heard %v", r.events)
			return -1
		}
	}
}

func appTurnDone(event string, _ any) bool { return event == appwire.EventTurnDone }

func fakeOpener(conv *fakeConversation, opens *int, releases *int, mu *sync.Mutex) appOpener {
	return func(ask appOpenAsk) (*appConn, error) {
		mu.Lock()
		*opens++
		mu.Unlock()
		return &appConn{
			Conv: conv, Workspace: ask.Workspace,
			Transcript: "/state/v3/places/proj/5e55a0f1/transcript.jsonl",
			Release:    func() { mu.Lock(); *releases++; mu.Unlock() },
		}, nil
	}
}

// AN AGENT'S TURN ON THE WIRE. Every session event a turn streams lands as the
// protocol's event, in order: the agent goes to work, the turn starts, chunks
// of the reply are folded per kind, tools begin and end with their outcome, a
// task's change is said once, the turn ends, and the agent is idle again.
func TestAppSendMapsSessionEventsOntoTurnEvents(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{
		{Kind: session.EventReasoning, Text: "hm"},
		{Kind: session.EventReasoning, Text: "m"},
		{Kind: session.EventTextDelta, Text: "hello"},
		{Kind: session.EventTextDelta, Text: " world"},
		{Kind: session.EventToolBegin, CallID: "c1", Tool: "read", Hint: "read go.mod"},
		{Kind: session.EventToolEnd, CallID: "c1", Tool: "read", Hint: "12 lines"},
		{Kind: session.EventToolBegin, CallID: "c2", Tool: "bash", Hint: "bash go test"},
		{Kind: session.EventToolFailed, CallID: "c2", Tool: "bash", Hint: "exit status 1"},
		{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 3, Title: "fix the parser", State: session.TaskRunning}},
		{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 3, Title: "fix the parser", State: session.TaskRunning}},
		{Kind: session.EventTitleChanged, Text: "Parser fix"},
		{Kind: session.EventTurnDone},
	}}
	var mu sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	backend := newAppBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()

	agent, err := backend.CreateAgent(t.TempDir(), "deepseek/deepseek-v4-flash")
	if err != nil {
		t.Fatal(err)
	}
	if agent.ID != "5e55a0f1" || agent.State != appwire.StateIdle || agent.Model != "deepseek/deepseek-v4-flash" {
		t.Fatalf("created = %+v", agent)
	}
	turn, err := backend.Send(agent.ID, "fix it")
	if err != nil || turn == 0 {
		t.Fatalf("send = %d, %v", turn, err)
	}
	done := rec.waitFor(t, 5*time.Second, appTurnDone)
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		changed, ok := data.(appwire.AgentChanged)
		return event == appwire.EventAgentChanged && ok && changed.Agent.State == appwire.StateIdle && changed.Agent.Title == "Parser fix"
	})

	rec.mu.Lock()
	defer rec.mu.Unlock()
	// The first event is the create's own agent.changed.
	got := rec.events[1 : done+2]
	want := []string{
		appwire.EventAgentChanged, appwire.EventTurnStarted,
		appwire.EventTurnDelta, appwire.EventTurnDelta,
		appwire.EventTurnTool, appwire.EventTurnTool, appwire.EventTurnTool, appwire.EventTurnTool,
		appwire.EventTaskChanged, appwire.EventAgentChanged, appwire.EventTurnDone, appwire.EventAgentChanged,
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("events:\n got %v\nwant %v", got, want)
	}
	data := rec.data[1 : done+2]
	if s := data[0].(appwire.AgentChanged).Agent.State; s != appwire.StateWorking {
		t.Fatalf("first change = %s, want working", s)
	}
	if d := data[1].(appwire.TurnStarted); d.Turn != turn || d.Agent != agent.ID {
		t.Fatalf("turn.started = %+v", d)
	}
	if d := data[2].(appwire.TurnDelta); d.Kind != appwire.DeltaThinking || d.Text != "hmm" {
		t.Fatalf("thinking = %+v", d)
	}
	if d := data[3].(appwire.TurnDelta); d.Kind != appwire.DeltaText || d.Text != "hello world" {
		t.Fatalf("text = %+v", d)
	}
	begin := data[4].(appwire.TurnTool)
	end := data[5].(appwire.TurnTool)
	failed := data[7].(appwire.TurnTool)
	if begin.Phase != appwire.PhaseBegin || begin.Call != "c1" || begin.Name != "read" || begin.OK != nil {
		t.Fatalf("begin = %+v", begin)
	}
	if end.Phase != appwire.PhaseEnd || end.OK == nil || !*end.OK || end.Summary != "12 lines" {
		t.Fatalf("end = %+v", end)
	}
	if failed.Phase != appwire.PhaseEnd || failed.OK == nil || *failed.OK || failed.Summary != "exit status 1" {
		t.Fatalf("failed = %+v", failed)
	}
	if task := data[8].(appwire.TaskChanged).Task; task.ID != "3" || task.Agent != agent.ID || task.State != appwire.TaskRunning || task.Title != "fix the parser" {
		t.Fatalf("task = %+v", task)
	}
	if d := data[10].(appwire.TurnDone); !d.OK || d.Turn != turn || d.Error != "" {
		t.Fatalf("turn.done = %+v", d)
	}
}

// A turn that errs ends not ok with the reason, and a second send while one is
// running is refused busy rather than queued behind it.
func TestAppSendRefusesBusyAndReportsAFailedTurn(t *testing.T) {
	hold := make(chan struct{})
	conv := &fakeConversation{hold: hold, script: []session.Event{{Kind: session.EventError, Err: errors.New("the provider refused")}}}
	var mu sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	backend := newAppBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()
	agent, err := backend.CreateAgent(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Send(agent.ID, "one"); err != nil {
		t.Fatal(err)
	}
	_, err = backend.Send(agent.ID, "two")
	var wire *appwire.Error
	if !errors.As(err, &wire) || wire.Code != appwire.CodeBusy {
		t.Fatalf("second send = %v, want busy", err)
	}
	if err := backend.Stop(agent.ID); err != nil || conv.interrupted != 1 {
		t.Fatalf("stop = %v, interrupted %d", err, conv.interrupted)
	}
	close(hold)
	i := rec.waitFor(t, 5*time.Second, appTurnDone)
	rec.mu.Lock()
	d := rec.data[i].(appwire.TurnDone)
	rec.mu.Unlock()
	if d.OK || !strings.Contains(d.Error, "the provider refused") {
		t.Fatalf("turn.done = %+v", d)
	}
}

// IDLE CONVERSATIONS ARE LET GO AND COME BACK BY THEMSELVES. One past the idle
// rule is released with its state left idle; one still waiting on the person is
// kept; the next send opens the conversation again without being asked.
func TestAppLetsAnIdleConversationGoAndTheNextSendReopensIt(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
	var mu sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	backend := newAppBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()
	clock := time.Now()
	backend.now = func() time.Time { return clock }
	agent, err := backend.CreateAgent(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}

	setNeeds := func(v bool) { conv.mu.Lock(); conv.needs = v; conv.mu.Unlock() }
	setNeeds(true)
	clock = clock.Add(appAgentIdle + time.Second)
	backend.sweepOnce()
	mu.Lock()
	kept := releases == 0
	mu.Unlock()
	if !kept {
		t.Fatal("a conversation waiting on the person was let go")
	}
	setNeeds(false)
	backend.sweepOnce()
	mu.Lock()
	if releases != 1 {
		t.Fatalf("released %d times, want once", releases)
	}
	mu.Unlock()
	backend.mu.Lock()
	state := backend.agents[agent.ID].view.State
	backend.mu.Unlock()
	if state != appwire.StateIdle {
		t.Fatalf("state after letting go = %s", state)
	}

	if _, err := backend.Send(agent.ID, "again"); err != nil {
		t.Fatalf("send after letting go = %v", err)
	}
	rec.waitFor(t, 5*time.Second, appTurnDone)
	mu.Lock()
	defer mu.Unlock()
	if opens != 2 {
		t.Fatalf("opened %d times, want the create and the reopen", opens)
	}
}

// agents.close gives the conversation back, says closed, and leaves the agent
// openable; an id nobody has is not found.
func TestAppCloseSaysClosedAndUnknownIdsAreNotFound(t *testing.T) {
	conv := &fakeConversation{}
	var mu sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	backend := newAppBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()
	t.Setenv(home.EnvVar, t.TempDir())
	agent, err := backend.CreateAgent(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.CloseAgent(agent.ID); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.State == appwire.StateClosed
	})
	if releases != 1 {
		t.Fatalf("released %d", releases)
	}
	if _, err := backend.OpenAgent(agent.ID); err != nil {
		t.Fatalf("reopen = %v", err)
	}
	_, err = backend.OpenAgent("feedface")
	var wire *appwire.Error
	if !errors.As(err, &wire) || wire.Code != appwire.CodeNotFound {
		t.Fatalf("unknown = %v", err)
	}
}

// A REAL CONVERSATION, HELD IN THIS PROCESS. The backend builds it with the
// same assembly a host uses, over an in-memory pipe, against a scripted model
// endpoint; a sent message comes back as the model's reply on the wire, the
// conversation is on disk for agents.list, and closing the backend lets it go.
func TestAppHoldsARealConversationInProcess(t *testing.T) {
	root := resolvedTempDir(t)
	_, workspace := wakeEnvironment(t, root)
	rec := newAppRecorder()
	backend := newAppBackend(rec, appOpenInProcess)
	agent, err := backend.CreateAgent(workspace, "")
	if err != nil {
		backend.Close()
		t.Fatalf("create = %v", err)
	}
	if agent.Workspace != workspace || agent.ID == "" {
		backend.Close()
		t.Fatalf("agent = %+v", agent)
	}
	turn, err := backend.Send(agent.ID, "say done")
	if err != nil {
		backend.Close()
		t.Fatalf("send = %v", err)
	}
	i := rec.waitFor(t, 30*time.Second, appTurnDone)
	rec.mu.Lock()
	done := rec.data[i].(appwire.TurnDone)
	var text strings.Builder
	for j, event := range rec.events {
		if d, ok := rec.data[j].(appwire.TurnDelta); ok && event == appwire.EventTurnDelta && d.Kind == appwire.DeltaText && d.Turn == turn {
			text.WriteString(d.Text)
		}
	}
	rec.mu.Unlock()
	if !done.OK || done.Turn != turn {
		backend.Close()
		t.Fatalf("turn.done = %+v", done)
	}
	if !strings.Contains(text.String(), "done") {
		backend.Close()
		t.Fatalf("the reply on the wire was %q", text.String())
	}
	agents, err := backend.ListAgents(workspace)
	if err != nil || len(agents) != 1 || agents[0].ID != agent.ID {
		backend.Close()
		t.Fatalf("agents.list = %+v, %v", agents, err)
	}
	tasks, err := backend.ListTasks(agent.ID)
	if err != nil || len(tasks) != 0 {
		backend.Close()
		t.Fatalf("tasks.list = %+v, %v", tasks, err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	if live := backend.liveOf(backend.agents[agent.ID]); live != nil {
		t.Fatal("closing the backend kept a conversation")
	}
	if !backend.assembled.Load() {
		t.Fatal("the backend did not record that it built a conversation")
	}
}

func appAskEvent(kind string) func(string, any) bool {
	return func(event string, _ any) bool { return event == kind }
}

// ASKS ARE THE ENGINE'S QUESTIONS, OPENED AND CLOSED ON ITS QUESTION LANE. An
// approval arrives with the engine's own choices and puts the agent at
// needs_you; an option it does not offer is refused; answering one goes
// through the engine's answer door and closes it by the app; one answered in
// another window closes by elsewhere; and an ask no longer open is not found.
func TestAppAsksOpenAnswerAndCloseThroughTheQuestionLane(t *testing.T) {
	conv := &fakeConversation{}
	conv.onAnswer = func(answer session.Answer) {
		q := session.Question{Kind: answer.Kind, ID: answer.ID}
		conv.raise(session.Event{Kind: session.EventQuestionAnswered, Question: &q, Answer: &answer})
	}
	var mu sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	backend := newAppBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()
	agent, err := backend.CreateAgent(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}

	approval := session.Question{ID: 4, Kind: session.QuestionConsent, Head: "bash rm -rf build", Reason: "bash asks first", Options: session.AnswerOptions(session.QuestionConsent)}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &approval})
	i := rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.State == appwire.StateNeedsYou
	})
	rec.mu.Lock()
	opened := rec.data[i].(appwire.AskOpened).Ask
	rec.mu.Unlock()
	if opened.ID != "consent:4" || opened.Kind != appwire.AskApproval || opened.Title != "bash rm -rf build" || opened.Detail != "bash asks first" || len(opened.Options) != 3 || opened.Options[2].Label != "deny" {
		t.Fatalf("ask = %+v", opened)
	}
	asks, err := backend.ListAsks(agent.ID)
	if err != nil || len(asks) != 1 || asks[0].ID != "consent:4" {
		t.Fatalf("asks.list = %+v, %v", asks, err)
	}
	var wire *appwire.Error
	if err := backend.AnswerAsk(agent.ID, "consent:4", "9", ""); !errors.As(err, &wire) || wire.Code != appwire.CodeBadParams {
		t.Fatalf("answer with an option not offered = %v", err)
	}
	if err := backend.AnswerAsk(agent.ID, "consent:4", "1", ""); err != nil {
		t.Fatal(err)
	}
	closed := rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskClosed))
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.State == appwire.StateIdle
	})
	rec.mu.Lock()
	if c := rec.data[closed].(appwire.AskClosed); c.ID != "consent:4" || c.By != appwire.ClosedByApp {
		rec.mu.Unlock()
		t.Fatalf("ask.closed = %+v", c)
	}
	rec.mu.Unlock()
	conv.mu.Lock()
	got := conv.answers[0]
	conv.mu.Unlock()
	if got.Kind != session.QuestionConsent || got.ID != 4 || got.Key != "1" || len(got.Picked) != 1 {
		t.Fatalf("the engine was handed %+v", got)
	}

	question := session.Question{ID: 7, Kind: session.QuestionAsk, Head: "Which database?", Options: []session.AnswerOption{{Key: "1", Label: "sqlite"}}}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &question})
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		o, ok := data.(appwire.AskOpened)
		return ok && o.Ask.ID == "ask:7" && o.Ask.Kind == appwire.AskQuestion
	})
	conv.raise(session.Event{Kind: session.EventQuestionAnswered, Question: &question})
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AskClosed)
		return ok && c.ID == "ask:7" && c.By == appwire.ClosedByElsewhere
	})
	if err := backend.AnswerAsk(agent.ID, "ask:7", "1", ""); !errors.As(err, &wire) || wire.Code != appwire.CodeNotFound {
		t.Fatalf("answering a closed ask = %v", err)
	}
}

// A CONVERSATION WHOSE ENGINE DIES READS STOPPED. Its open asks close as
// stopped, and the next send opens it again and works.
func TestAppADeadConversationReadsStoppedAndTheNextSendReopensIt(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
	var mu sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	backend := newAppBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()
	agent, err := backend.CreateAgent(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	q := session.Question{ID: 2, Kind: session.QuestionConsent, Head: "bash make", Options: session.AnswerOptions(session.QuestionConsent)}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &q})
	rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))

	conv.mu.Lock()
	end := conv.endLane
	conv.mu.Unlock()
	end()
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AskClosed)
		return ok && c.ID == "consent:2" && c.By == appwire.ClosedByStopped
	})
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.State == appwire.StateStopped
	})
	if asks, _ := backend.ListAsks(agent.ID); len(asks) != 0 {
		t.Fatalf("a stopped agent still lists %d asks", len(asks))
	}
	agents, _ := backend.ListAgents("")
	for _, listed := range agents {
		if listed.ID == agent.ID && listed.State != appwire.StateStopped {
			t.Fatalf("listed as %s", listed.State)
		}
	}
	if _, err := backend.Send(agent.ID, "again"); err != nil {
		t.Fatalf("send after the engine died = %v", err)
	}
	rec.waitFor(t, 5*time.Second, appTurnDone)
	mu.Lock()
	defer mu.Unlock()
	if opens != 2 {
		t.Fatalf("opened %d times, want the create and the reopen", opens)
	}
}

// A REAL APPROVAL, ANSWERED FROM THE APP. The engine is built in this process
// with every bash call asking first; the scripted model asks for one; the ask
// opens with the engine's own choices, `allow once` is sent through the
// engine's answer door, the ask closes by the app, the command runs and the
// turn ends well. The conversation records the workspace the app asked for as
// where the person stood, not the directory the app was started in.
func TestAppAnswersARealApprovalAndRecordsTheWorkspaceAsTheLaunchFolder(t *testing.T) {
	root := resolvedTempDir(t)
	_, started := wakeEnvironment(t, root)
	model := consentSource(t, "printf app-ask-probe")
	t.Cleanup(model.Close)
	t.Setenv("CODEAF_BASE_URL", model.URL)
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "config.json"), []byte(`{"tools.approvalMode":"prompt"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "elsewhere")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	rec := newAppRecorder()
	backend := newAppBackend(rec, appOpenInProcess)
	defer backend.Close()
	agent, err := backend.CreateAgent(workspace, "")
	if err != nil {
		t.Fatalf("create = %v", err)
	}
	backend.mu.Lock()
	transcript := backend.agents[agent.ID].transcript
	backend.mu.Unlock()
	meta, err := session.LoadMeta(filepath.Dir(transcript))
	if err != nil || meta.LaunchDir != workspace {
		t.Fatalf("launch folder = %q (app started in %q), %v", meta.LaunchDir, started, err)
	}

	if _, err := backend.Send(agent.ID, "run the probe"); err != nil {
		t.Fatal(err)
	}
	i := rec.waitFor(t, 30*time.Second, appAskEvent(appwire.EventAskOpened))
	rec.mu.Lock()
	ask := rec.data[i].(appwire.AskOpened).Ask
	rec.mu.Unlock()
	if ask.Kind != appwire.AskApproval || len(ask.Options) == 0 || ask.Options[0].Label != "allow once" {
		t.Fatalf("ask = %+v", ask)
	}
	if err := backend.AnswerAsk(agent.ID, ask.ID, ask.Options[0].ID, ""); err != nil {
		t.Fatalf("answer = %v", err)
	}
	rec.waitFor(t, 30*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AskClosed)
		return ok && c.ID == ask.ID && c.By == appwire.ClosedByApp
	})
	d := rec.waitFor(t, 30*time.Second, appTurnDone)
	rec.mu.Lock()
	done := rec.data[d].(appwire.TurnDone)
	var ran bool
	for j, event := range rec.events {
		if tool, ok := rec.data[j].(appwire.TurnTool); ok && event == appwire.EventTurnTool && tool.Phase == appwire.PhaseEnd && tool.Name == "bash" && tool.OK != nil && *tool.OK {
			ran = true
		}
	}
	rec.mu.Unlock()
	if !done.OK || !ran {
		t.Fatalf("turn.done = %+v, bash ran = %v", done, ran)
	}
}

// A CREW THAT CLOSES OR DISAPPEARS IS REMOVED. Once the app has listed the
// crews, a team closed in the teams file is announced as crew.removed with its
// id, and a renamed one as crew.changed.
func TestAppAnnouncesAClosedCrewAsRemoved(t *testing.T) {
	profile := t.TempDir()
	t.Setenv(config.ProfileDirEnv, profile)
	member := filepath.Join(profile, "places", "p", "0123abcd", "transcript.jsonl")
	teamsNow := []teamstore.Team{
		{ID: "0a0a0a0a0a0a", Name: "harbor", Members: []teamstore.Member{{Key: member, File: member}}},
		{ID: "0b0b0b0b0b0b", Name: "dock", Members: []teamstore.Member{{Key: member, File: member}}},
	}
	if err := teamstore.Save(profile, teamsNow); err != nil {
		t.Fatal(err)
	}
	rec := newAppRecorder()
	backend := newAppBackend(rec, nil)
	defer backend.Close()
	crews, err := backend.ListCrews()
	if err != nil || len(crews) != 2 {
		t.Fatalf("crews = %+v, %v", crews, err)
	}
	// The poll compares the file's stamp, so the change must land on a later one.
	time.Sleep(20 * time.Millisecond)
	teamsNow[0].State = teamstore.TeamClosed
	teamsNow[1].Name = "pier"
	if err := teamstore.Save(profile, teamsNow); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 4*appCrewPoll, func(event string, data any) bool {
		removed, ok := data.(appwire.CrewRemoved)
		return ok && removed.ID == "0a0a0a0a0a0a"
	})
	rec.waitFor(t, 4*appCrewPoll, func(event string, data any) bool {
		changed, ok := data.(appwire.CrewChanged)
		return ok && changed.Crew.Name == "pier"
	})
}
