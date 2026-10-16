package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/provider"
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
	routing     session.TaskRoutingView
	routingErr  error
	routingActs []session.TaskRoutingAction
	script      []session.Event
	hold        chan struct{}
	submits     int
	interrupted int
	needs       bool
	tasks       chan session.Event
	questions   chan session.Event
	endLane     func()
	answers     []session.Answer
	model       string
	effort      string
	rail        float64
	remembered  []session.ConsentScope
	texts       []string
	submissions []session.Submission
	cost        float64
	// The task, quick task and process doors' scripted answers, and what was
	// asked of them.
	appTasks    []session.AppTaskView
	quickTasks  []session.AppQuickTaskView
	intents     []session.AppTaskIntent
	undoChanged []string
	jobs        []session.JobNotice
	cancels     []string
	moves       int
	// room is the lane one task's calls are watched on.
	room chan session.Event
	// steer is what a steer answers with, steered the words steered, and
	// retried the tasks run again.
	steer   chan session.Event
	steered []string
	retried []uint64
	// onAnswer, when set, is what the engine does with an answer.
	onAnswer func(session.Answer)
}

func (f *fakeConversation) WatchTaskRoom(uint64) (<-chan session.Event, func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	lane := make(chan session.Event, 8)
	f.room = lane
	var once sync.Once
	return lane, func() { once.Do(func() { close(lane) }) }, nil
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
	f.texts = append(f.texts, text)
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

// SubmitMessage is Submit with what the door said about the message kept
// beside the words, so a test can read what the engine was handed.
func (f *fakeConversation) SubmitMessage(ctx context.Context, message session.Submission) (<-chan session.Event, error) {
	f.mu.Lock()
	f.submissions = append(f.submissions, message)
	f.mu.Unlock()
	return f.Submit(ctx, message.Text)
}
func (f *fakeConversation) Interrupt() { f.mu.Lock(); f.interrupted++; f.mu.Unlock() }
func (f *fakeConversation) Model() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.model != "" {
		return f.model
	}
	return "deepseek/deepseek-v4-flash"
}
func (f *fakeConversation) SetModel(model string) { f.mu.Lock(); f.model = model; f.mu.Unlock() }
func (f *fakeConversation) ReasoningFor(model string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.effort
}
func (f *fakeConversation) SetReasoningFor(model, level string) {
	f.mu.Lock()
	f.effort = level
	f.mu.Unlock()
}
func (f *fakeConversation) SetSpendRail(usd float64) error {
	f.mu.Lock()
	f.rail = usd
	f.mu.Unlock()
	return nil
}
func (f *fakeConversation) ResolveConsentRemember(id uint64, allow bool, scope session.ConsentScope) {
	f.mu.Lock()
	f.remembered = append(f.remembered, scope)
	f.mu.Unlock()
}
func (f *fakeConversation) AppTasks() ([]session.AppTaskView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]session.AppTaskView(nil), f.appTasks...), nil
}
func (f *fakeConversation) AppTask(id string) (session.AppTaskView, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, view := range f.appTasks {
		if view.ID == id {
			return view, true, nil
		}
	}
	return session.AppTaskView{}, false, nil
}
func (f *fakeConversation) AppTaskAction(intent session.AppTaskIntent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.intents = append(f.intents, intent)
	return nil
}
func (f *fakeConversation) TaskRouting(id string, changes int) (session.TaskRoutingView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	view := f.routing
	view.Task = id
	return view, nil
}
func (f *fakeConversation) TaskRoutingAct(_ context.Context, id string, act session.TaskRoutingAction) (session.TaskRoutingResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routingActs = append(f.routingActs, act)
	if f.routingErr != nil {
		return session.TaskRoutingResult{}, f.routingErr
	}
	return session.TaskRoutingResult{View: f.routing, Applied: []crewroute.Seat{crewroute.Worker}}, nil
}
func (f *fakeConversation) TaskRoutingOptions(id string) (session.TaskRoutingOptions, error) {
	return session.TaskRoutingOptions{Task: id, Scope: session.TaskRoutingNextStep, Pick: true,
		Stronger: &session.TaskRoutingPreview{Moves: []session.TaskRoutingMove{{Seat: crewroute.Worker, From: "glm-flash", To: "sonnet", ToPrice: &crewroute.RoutePrice{Prompt: 3e-6, Completion: 15e-6}}}}}, nil
}
func (f *fakeConversation) AppQuickTasks() ([]session.AppQuickTaskView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]session.AppQuickTaskView(nil), f.quickTasks...), nil
}
func (f *fakeConversation) AppQuickTaskAction(intent session.AppTaskIntent) ([]string, []string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.intents = append(f.intents, intent)
	if intent.Action == "undo" && len(f.undoChanged) > 0 {
		return nil, append([]string(nil), f.undoChanged...), nil
	}
	if intent.Action == "undo" {
		for i := range f.quickTasks {
			if f.quickTasks[i].ID == intent.ID {
				f.quickTasks[i].State = "undone"
			}
		}
	}
	return nil, nil, nil
}
func (f *fakeConversation) Steer(text string) (<-chan session.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steered = append(f.steered, text)
	if f.steer == nil {
		return nil, session.ErrNothingToSteer
	}
	return f.steer, nil
}

func (f *fakeConversation) RetryTask(id uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retried = append(f.retried, id)
	return nil
}

func (f *fakeConversation) MoveTurnToTask() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moves++
	return nil
}
func (f *fakeConversation) JobNotices() ([]session.JobNotice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]session.JobNotice(nil), f.jobs...), nil
}
func (f *fakeConversation) RerunProcess(id int) (session.JobNotice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, job := range f.jobs {
		if job.ID == id {
			job.ID, job.State = id+100, session.JobRunning
			return job, nil
		}
	}
	return session.JobNotice{}, errors.New("no such process")
}
func (f *fakeConversation) Cancel(id string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancels = append(f.cancels, id)
	return "stopped", nil
}
func (f *fakeConversation) Usage() session.Usage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return session.Usage{CostUSD: f.cost}
}
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
	// EVERY EVENT IS ITS CONTRACT'S SHAPE: data of any other type is a wire
	// break the app would reject, so a test that provokes one fails here.
	if want := appwire.EventShape(event); want == nil || reflect.TypeOf(data) != want {
		panic(fmt.Sprintf("event %s carried %T, and its contract is %v", event, data, want))
	}
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

// appFakeTranscript is the one conversation a fake opener holds.
const appFakeTranscript = "/state/v3/places/proj/5e55a0f1/transcript.jsonl"

// newFakeBackend is a backend over a fake opener, minting the fake's own
// folder rather than one on disk.
//
// Every fake backend keeps its own app file, because every fake agent has the
// same id and a budget one test gave it must not reach the next.
func newFakeBackend(rec appwire.Emitter, open appOpener) *appBackend {
	backend := newAppBackend(rec, open)
	backend.mint = func(string) (string, error) { return appFakeTranscript, nil }
	backend.state = &appState{path: filepath.Join(os.TempDir(), "codeaf-app-test-"+strconv.Itoa(os.Getpid()), strconv.FormatUint(appFakeStates.Add(1), 10), appStateName)}
	return backend
}

// appFakeStates numbers the fake backends' app files.
var appFakeStates atomic.Uint64

// mustCreate creates an agent and, when it was given no words, waits for its
// conversation to be open, which agents.create does behind its answer.
func mustCreate(t *testing.T, backend *appBackend, ask appwire.CreateAsk) (appwire.Agent, error) {
	t.Helper()
	agent, err := backend.CreateAgent(ask)
	if err != nil || strings.TrimSpace(ask.Text) != "" {
		return agent, err
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		a, _ := backend.find(agent.ID)
		if a != nil && backend.liveOf(a) != nil {
			var view appwire.Agent
			backend.locked(func() { view = a.view })
			return view, nil
		}
		if time.Now().After(deadline) {
			t.Fatalf("the agent's conversation never opened")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func fakeOpener(conv *fakeConversation, opens *int, releases *int, mu *sync.Mutex) appOpener {
	return func(ask appOpenAsk) (*appConn, error) {
		mu.Lock()
		*opens++
		mu.Unlock()
		return &appConn{
			Conv: conv, Workspace: ask.Workspace,
			Transcript: appFakeTranscript,
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
	backend := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()

	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir(), Model: "deepseek/deepseek-v4-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if agent.ID != "5e55a0f1" || agent.State != appwire.StateIdle || agent.Model != "deepseek/deepseek-v4-flash" {
		t.Fatalf("created = %+v", agent)
	}
	turn, err := backend.Send(agent.ID, "fix it", "")
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
	// The first two events are the create's own agent.changed and the one
	// that says its conversation opened behind the answer.
	got := rec.events[2 : done+2]
	want := []string{
		appwire.EventAgentChanged, appwire.EventTurnStarted, appwire.EventEntryAdded,
		appwire.EventTurnThinking, appwire.EventTurnThinkingDone, appwire.EventTurnDelta, appwire.EventEntryAdded,
		appwire.EventTurnTool, appwire.EventTurnTool, appwire.EventTurnTool, appwire.EventTurnTool,
		appwire.EventTaskChanged, appwire.EventAgentChanged, appwire.EventTurnDone, appwire.EventAgentChanged,
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("events:\n got %v\nwant %v", got, want)
	}
	data := rec.data[2 : done+2]
	if s := data[0].(appwire.AgentChanged).Agent.State; s != appwire.StateWorking {
		t.Fatalf("first change = %s, want working", s)
	}
	if d := data[1].(appwire.TurnStarted); d.Turn != turn || d.Agent != agent.ID {
		t.Fatalf("turn.started = %+v", d)
	}
	if e := data[2].(appwire.EntryAdded).Entry; e.Kind != appwire.EntryUser || e.ID != "u0" || e.Text != "fix it" || e.Turn != turn || e.Agent != agent.ID {
		t.Fatalf("the person's entry = %+v", e)
	}
	// The thought is said live and never kept; the words before a tool call
	// are an update.
	if d := data[3].(appwire.TurnThinking); d.Text != "hmm" || d.Turn != turn {
		t.Fatalf("thinking = %+v", d)
	}
	if d := data[4].(appwire.TurnThinkingDone); d.Seconds < 1 {
		t.Fatalf("thinking done = %+v", d)
	}
	if d := data[5].(appwire.TurnDelta); d.Kind != appwire.DeltaText || d.Text != "hello world" {
		t.Fatalf("text = %+v", d)
	}
	if e := data[6].(appwire.EntryAdded).Entry; e.Kind != appwire.EntryUpdate || e.ID != "a0.0" || e.Msg == nil || *e.Msg != 0 || e.Text != "hello world" || e.Thinking != "" {
		t.Fatalf("the update entry = %+v", e)
	}
	begin := data[7].(appwire.TurnTool)
	end := data[8].(appwire.TurnTool)
	failed := data[10].(appwire.TurnTool)
	if begin.Phase != appwire.PhaseBegin || begin.Call != "c1" || begin.Name != "read" || begin.Title != "read go.mod" || begin.OK != nil {
		t.Fatalf("begin = %+v", begin)
	}
	if end.Phase != appwire.PhaseEnd || end.OK == nil || !*end.OK || end.Summary != "12 lines" {
		t.Fatalf("end = %+v", end)
	}
	if failed.Phase != appwire.PhaseEnd || failed.OK == nil || *failed.OK || failed.Summary != "exit status 1" {
		t.Fatalf("failed = %+v", failed)
	}
	if task := data[11].(appwire.TaskChanged).Task; task.ID != agent.ID+":3" || task.Agent != agent.ID || task.State != appwire.TaskRunning || task.Title != "fix the parser" {
		t.Fatalf("task = %+v", task)
	}
	if d := data[13].(appwire.TurnDone); !d.OK || d.Turn != turn || d.Error != "" {
		t.Fatalf("turn.done = %+v", d)
	}
}

// A second send while a turn runs is refused busy rather than queued behind
// it, and a turn the person stops ends stopped, said once as a receipt.
func TestAppSendRefusesBusyAndReportsAFailedTurn(t *testing.T) {
	hold := make(chan struct{})
	conv := &fakeConversation{hold: hold, script: []session.Event{{Kind: session.EventError, Err: errors.New("the provider refused")}}}
	var mu sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	backend := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Send(agent.ID, "one", ""); err != nil {
		t.Fatal(err)
	}
	_, err = backend.Send(agent.ID, "two", "")
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
	// The person stopped it, so it ends stopped — with one receipt saying so,
	// and no error entry and no answer made of the word.
	if d.OK || d.ErrorKind != appwire.ErrorStopped || d.Error != "stopped" {
		t.Fatalf("turn.done = %+v", d)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	receipts := 0
	for _, data := range rec.data {
		if e, ok := data.(appwire.EntryAdded); ok {
			switch e.Entry.Kind {
			case appwire.EntryFooter:
				if e.Entry.Footer == appwire.FooterStoppedByYou && strings.HasPrefix(e.Entry.Text, "Stopped by you · ") {
					receipts++
				}
			case appwire.EntryError, appwire.EntryAnswer:
				t.Fatalf("a stopped turn said %+v", e.Entry)
			}
		}
	}
	if receipts != 1 {
		t.Fatalf("%d stop receipts", receipts)
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
	backend := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()
	clock := time.Now()
	backend.now = func() time.Time { return clock }
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
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

	if _, err := backend.Send(agent.ID, "again", ""); err != nil {
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
	backend := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()
	t.Setenv(home.EnvVar, t.TempDir())
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
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
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		backend.Close()
		t.Fatalf("create = %v", err)
	}
	if agent.Workspace != workspace || agent.ID == "" {
		backend.Close()
		t.Fatalf("agent = %+v", agent)
	}
	turn, err := backend.Send(agent.ID, "say done", "")
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

	// The transcript reads back with nothing held: the person's words, then
	// the reply, oldest first; a page of one is the newest, with more before.
	entries, more, err := backend.Entries(agent.ID, "", appwire.MaxEntries)
	if err != nil || more || len(entries) < 2 {
		t.Fatalf("entries = %+v, more %v, %v", entries, more, err)
	}
	if first := entries[0]; first.Kind != appwire.EntryUser || first.Text != "say done" {
		t.Fatalf("first entry = %+v", first)
	}
	last := entries[len(entries)-1]
	if last.Kind != appwire.EntryAnswer || !strings.Contains(last.Text, "done") {
		t.Fatalf("last entry = %+v", last)
	}
	page, more, err := backend.Entries(agent.ID, "", 1)
	if err != nil || !more || len(page) != 1 || page[0].ID != last.ID {
		t.Fatalf("a page of one = %+v, more %v, %v", page, more, err)
	}
	earlier, _, err := backend.Entries(agent.ID, last.ID, 1)
	if err != nil || len(earlier) != 1 || earlier[0].ID != entries[len(entries)-2].ID {
		t.Fatalf("the page before = %+v, %v", earlier, err)
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
	backend := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
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
	if opened.ID != "consent:4" || opened.Kind != appwire.AskApproval || opened.Subkind != appwire.SubkindPermission || opened.Title != "bash rm -rf build" || opened.Detail != "bash asks first" || len(opened.Options) != 3 ||
		opened.Options[0].ID != appwire.OptionAllowOnce || opened.Options[1].ID != appwire.OptionDeny || opened.Options[1].Label != "deny" || opened.Options[2].ID != appwire.OptionAlways {
		t.Fatalf("ask = %+v", opened)
	}
	asks, err := backend.ListAsks(agent.ID)
	if err != nil || len(asks) != 1 || asks[0].ID != "consent:4" {
		t.Fatalf("asks.list = %+v, %v", asks, err)
	}
	var wire *appwire.Error
	if err := backend.AnswerAsk(agent.ID, "consent:4", "1", ""); !errors.As(err, &wire) || wire.Code != appwire.CodeBadParams {
		t.Fatalf("answer with the engine's key instead of the app's = %v", err)
	}
	if err := backend.AnswerAsk(agent.ID, "consent:4", appwire.OptionAllowOnce, ""); err != nil {
		t.Fatal(err)
	}
	closed := rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskClosed))
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.State == appwire.StateIdle
	})
	rec.mu.Lock()
	if c := rec.data[closed].(appwire.AskClosed); c.ID != "consent:4" || c.By != appwire.ClosedByApp || c.Option != appwire.OptionAllowOnce || c.At.IsZero() {
		rec.mu.Unlock()
		t.Fatalf("ask.closed = %+v", c)
	}
	receipt, ok := rec.data[closed+1].(appwire.EntryAdded)
	rec.mu.Unlock()
	if !ok || receipt.Entry.Kind != appwire.EntryReceipt || !strings.HasPrefix(receipt.Entry.Text, "Allowed once · you · ") {
		t.Fatalf("the receipt = %+v", receipt)
	}
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
	backend := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
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
	if _, err := backend.Send(agent.ID, "again", ""); err != nil {
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
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace})
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

	if _, err := backend.Send(agent.ID, "run the probe", ""); err != nil {
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

// A CREW THAT CLOSES IS SAID AGAIN AS CLOSED, AND ONE THAT DISAPPEARS IS
// REMOVED. Once the app has listed the crews, a team closed in the teams file is
// announced as crew.changed in the closed state (it stays readable and can be
// reopened), a renamed one as crew.changed, and one deleted from the file as
// crew.removed with its id.
func TestAppAnnouncesAClosedCrewAndRemovesADeletedOne(t *testing.T) {
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
		changed, ok := data.(appwire.CrewChanged)
		return ok && changed.Crew.ID == "0a0a0a0a0a0a" && changed.Crew.State == appwire.CrewClosed
	})
	rec.waitFor(t, 4*appCrewPoll, func(event string, data any) bool {
		changed, ok := data.(appwire.CrewChanged)
		return ok && changed.Crew.Name == "pier"
	})
	time.Sleep(20 * time.Millisecond)
	if err := teamstore.Save(profile, teamsNow[1:]); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 4*appCrewPoll, func(event string, data any) bool {
		removed, ok := data.(appwire.CrewRemoved)
		return ok && removed.ID == "0a0a0a0a0a0a"
	})
}

// CREATED WITH WORDS, AN AGENT STARTS AT ONCE AND IS NAMED BY THEM. No
// workspace is the home folder; the budget is the conversation's own limit,
// set before the first turn; the title is the first words cut at forty
// characters with an ellipsis; the turn starts with the words given.
func TestAppCreateWithTextStartsTheTurnAndNamesTheAgent(t *testing.T) {
	homeDir := resolvedTempDir(t)
	t.Setenv("HOME", homeDir)
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
	var mu sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	backend := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()
	budget := 3.0
	words := "Please fix the flaky parser test in the lexer package before lunch"
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Text: words, BudgetUSD: &budget})
	if err != nil {
		t.Fatal(err)
	}
	if agent.Workspace != homeDir {
		t.Fatalf("workspace = %q, want the home folder %q", agent.Workspace, homeDir)
	}
	if want := session.FirstMessageName(words); agent.Title != want || !strings.HasSuffix(want, "…") || len([]rune(want)) > 41 {
		t.Fatalf("title = %q, want %q", agent.Title, want)
	}
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		started, ok := data.(appwire.TurnStarted)
		return ok && started.Agent == agent.ID
	})
	rec.waitFor(t, 5*time.Second, appTurnDone)
	conv.mu.Lock()
	defer conv.mu.Unlock()
	if conv.rail != budget || len(conv.texts) != 1 || conv.texts[0] != words {
		t.Fatalf("rail %v, sent %q", conv.rail, conv.texts)
	}
}

// A FAILED TURN IS SORTED AND CAN BE RUN AGAIN. A provider's refusal ends the
// turn as provider with its status and its own words, and leaves an error
// entry; agents.retry sends the same words again; with nothing failed it is
// not found.
func TestAppSortsAProviderFailureAndRetriesIt(t *testing.T) {
	refused := &provider.APIError{Status: 429, Message: "upstream is rate limited"}
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventError, Err: refused}}}
	var mu sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	backend := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var wire *appwire.Error
	if _, err := backend.Retry(agent.ID); !errors.As(err, &wire) || wire.Code != appwire.CodeNotFound {
		t.Fatalf("retry with nothing failed = %v", err)
	}
	if _, err := backend.Send(agent.ID, "summarise the repo", ""); err != nil {
		t.Fatal(err)
	}
	i := rec.waitFor(t, 5*time.Second, appTurnDone)
	rec.mu.Lock()
	done := rec.data[i].(appwire.TurnDone)
	errorEntry, _ := rec.data[i-1].(appwire.EntryAdded)
	rec.mu.Unlock()
	if done.OK || done.ErrorKind != appwire.ErrorProvider || done.Status != 429 || done.Error != "upstream is rate limited" {
		t.Fatalf("turn.done = %+v", done)
	}
	if e := errorEntry.Entry; e.Kind != appwire.EntryFooter || e.Footer != appwire.FooterProviderFailed || e.ErrorKind != appwire.ErrorProvider || e.Status != 429 || e.Text != done.Error {
		t.Fatalf("the error entry = %+v", e)
	}
	// The next try goes through.
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.State == appwire.StateIdle
	})
	conv.mu.Lock()
	conv.script = []session.Event{{Kind: session.EventTurnDone}}
	conv.mu.Unlock()
	turn, err := backend.Retry(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		d, ok := data.(appwire.TurnDone)
		return ok && d.Turn == turn && d.OK
	})
	conv.mu.Lock()
	texts := append([]string(nil), conv.texts...)
	conv.mu.Unlock()
	if len(texts) != 2 || texts[1] != "summarise the repo" {
		t.Fatalf("sent %q", texts)
	}
	if _, err := backend.Retry(agent.ID); !errors.As(err, &wire) || wire.Code != appwire.CodeNotFound {
		t.Fatalf("retry after a good turn = %v", err)
	}
}

// The error kinds a turn's failure is sorted into.
func TestAppTurnFailureKinds(t *testing.T) {
	for _, c := range []struct {
		err    error
		kind   string
		status int
	}{
		{&provider.APIError{Status: 502, Message: "bad gateway"}, appwire.ErrorProvider, 502},
		{session.ErrSpendRail, appwire.ErrorBudget, 0},
		{context.Canceled, appwire.ErrorStopped, 0},
		{errors.New("disk full"), appwire.ErrorOther, 0},
	} {
		kind, status, words := appTurnFailure(session.Event{Kind: session.EventError, Err: c.err})
		if kind != c.kind || status != c.status || words == "" {
			t.Errorf("%v: kind %q status %d words %q", c.err, kind, status, words)
		}
	}
}

// agents.set_model moves the conversation and says so.
func TestAppSetModelMovesTheConversation(t *testing.T) {
	conv := &fakeConversation{}
	var mu sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	backend := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	moved, err := backend.SetModel(agent.ID, "qwen/qwen3-coder")
	if err != nil || moved.Model != "qwen/qwen3-coder" {
		t.Fatalf("set_model = %+v, %v", moved, err)
	}
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.Model == "qwen/qwen3-coder"
	})
}

// ALWAYS WRITES A RULE THE ENGINE HONOURS. The approval names the command the
// turn announced; answering always banks that one command line in the
// person's own profile and hands the engine the rule-scoped answer, so the
// next call like it is answered by the rule. A call that cannot be undone
// offers no always at all.
func TestAppAlwaysWritesABashRule(t *testing.T) {
	profile := t.TempDir()
	t.Setenv(config.ProfileDirEnv, profile)
	hold := make(chan struct{})
	conv := &fakeConversation{hold: hold, script: []session.Event{
		{Kind: session.EventConsentRequest, ID: 4, Tool: "bash", Args: `{"command":"go test ./internal/lexer/"}`},
	}}
	var mu sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	backend := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer func() { close(hold); backend.Close() }()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Send(agent.ID, "run the lexer tests", ""); err != nil {
		t.Fatal(err)
	}
	// The pump has seen the call once the turn's own event is behind it.
	deadline := time.Now().Add(5 * time.Second)
	for {
		backend.mu.Lock()
		_, seen := backend.agents[agent.ID].live.consents[4]
		backend.mu.Unlock()
		if seen {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the turn's approval call was never recorded")
		}
		time.Sleep(5 * time.Millisecond)
	}
	q := session.Question{ID: 4, Kind: session.QuestionConsent, Head: "bash go test ./internal/lexer/", Options: session.AnswerOptions(session.QuestionConsent)}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &q})
	rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	if err := backend.AnswerAsk(agent.ID, "consent:4", appwire.OptionAlways, ""); err != nil {
		t.Fatal(err)
	}
	conv.mu.Lock()
	remembered, answers := conv.remembered, len(conv.answers)
	conv.mu.Unlock()
	if len(remembered) != 1 || remembered[0] != session.ConsentRule || answers != 0 {
		t.Fatalf("remembered %v, answered %d through the question door", remembered, answers)
	}
	if rules := config.BashApprovalsAt(profile); !strings.Contains(rules, "go test ./internal/lexer/") {
		t.Fatalf("the profile's shell rules = %s", rules)
	}
	// The engine retires the question once the call goes on; the receipt says always.
	conv.raise(session.Event{Kind: session.EventQuestionWithdrawn, Question: &q})
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		e, ok := data.(appwire.EntryAdded)
		return ok && e.Entry.Kind == appwire.EntryReceipt && strings.HasPrefix(e.Entry.Text, "Always allowed · you · ")
	})

	final := session.Question{ID: 5, Kind: session.QuestionConsent, Head: "bash rm -rf dist", Stakes: session.StakesIrreversible, Options: session.AnswerOptions(session.QuestionConsent)}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &final})
	i := rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		o, ok := data.(appwire.AskOpened)
		return ok && o.Ask.ID == "consent:5"
	})
	rec.mu.Lock()
	ask := rec.data[i].(appwire.AskOpened).Ask
	rec.mu.Unlock()
	if !ask.Irreversible || len(ask.Options) != 2 || ask.Options[0].ID != appwire.OptionAllowOnce || ask.Options[1].ID != appwire.OptionDeny {
		t.Fatalf("an irreversible ask = %+v", ask)
	}
}

// settings.get reads the profile's rows for what the terminal shares, and the
// app's own file for the rest, with its defaults: a $20.00 day cap that is the
// app's and not the terminal's daily limit, a twelve-second proposal clock,
// seven idle days, and the home folder to work in.
func TestAppSettingsAndSpendReadTheProfile(t *testing.T) {
	profile := t.TempDir()
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	if err := os.WriteFile(config.BudgetConfigPath(profile), []byte(`{"model.talk":"qwen/qwen3-coder","tools.approvalMode":"deny","daily_budget_usd":12}`), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := newAppBackend(newAppRecorder(), nil)
	defer backend.Close()
	settings, err := backend.Settings()
	if err != nil {
		t.Fatal(err)
	}
	homeDir, _ := os.UserHomeDir()
	if settings.DefaultModel != "qwen/qwen3-coder" || settings.AsksBefore != "deny" || settings.BudgetUSD != nil || settings.DayCapUSD == nil || *settings.DayCapUSD != appDayCapDefault {
		t.Fatalf("settings = %+v (day cap %v)", settings, settings.DayCapUSD)
	}
	if settings.ProposedTaskClockS != 12 || settings.IdleCloseDays != 7 || settings.WorkFolder != homeDir || settings.TaskCapUSD == nil || *settings.TaskCapUSD != config.CrewTaskCapDefault {
		t.Fatalf("app settings = %+v", settings)
	}
	if daily, _ := config.DailyBudgetUSDAt(profile); daily != 12 {
		t.Fatalf("the terminal's daily limit moved to %v", daily)
	}
	spend, err := backend.SpendToday()
	if err != nil || spend.TodayUSD != 0 || spend.DayCapUSD == nil || *spend.DayCapUSD != appDayCapDefault {
		t.Fatalf("spend = %+v, %v", spend, err)
	}
}

// MODELS.LIST IS THE TERMINAL'S PICKER. With an OpenRouter-style profile — a
// key, the default service, its model list remembered from an earlier
// listing — the answer is that list as the picker offers it, each row's
// provider its own, and the model a new conversation runs on is marked
// default; a profile whose default no list names still gets it, marked.
func TestAppModelsListIsThePickersList(t *testing.T) {
	root := resolvedTempDir(t)
	_, _ = wakeEnvironment(t, root)
	settings, err := config.LoadKeyless()
	if err != nil {
		t.Fatal(err)
	}
	options := catalog.Options{BaseURL: settings.BaseURL, Dir: settings.ProfileDir}
	if err := catalog.Remember(options, []catalog.Model{
		{ID: "deepseek/deepseek-v4.1-flash", Name: "DeepSeek V4.1 Flash", Created: 1760000000, InputModalities: []string{"text"}, OutputModalities: []string{"text"}},
		{ID: "qwen/qwen3-coder", Name: "Qwen3 Coder", Created: -100, InputModalities: []string{"text"}, OutputModalities: []string{"text"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.BudgetConfigPath(settings.ProfileDir), []byte(`{"model.talk":"deepseek/deepseek-v4.1-flash"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := newAppBackend(newAppRecorder(), nil)
	defer backend.Close()
	models, err := backend.ListModels()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]appwire.Model{}
	defaults := 0
	for _, m := range models {
		byID[m.ID] = m
		if m.Default {
			defaults++
		}
	}
	if m := byID["deepseek/deepseek-v4.1-flash"]; !m.Default || m.Provider != "openrouter" || m.ProviderName != "OpenRouter" || defaults != 1 || m.ListedAt != 1760000000 {
		t.Fatalf("models = %+v", models)
	}
	if m := byID["qwen/qwen3-coder"]; m.Provider != "openrouter" || m.Default || m.ListedAt != 0 {
		t.Fatalf("models = %+v", models)
	}
	if err := os.WriteFile(config.BudgetConfigPath(settings.ProfileDir), []byte(`{"model.talk":"unlisted/model-2099-12-31"}`), 0600); err != nil {
		t.Fatal(err)
	}
	models, err = backend.ListModels()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range models {
		if m.ID == "unlisted/model-2099-12-31" {
			found = true
			if !m.Default || m.ListedAt != 0 {
				t.Fatalf("fallback invented listing time: %+v", m)
			}
		}
	}
	if !found {
		t.Fatal("missing default fallback")
	}

}

// A REAL ALWAYS, HONOURED BY THE ENGINE. Every shell call asks first; the
// first turn's call is answered always from the app, which writes the command
// down as a rule; the second turn makes the same call and is not asked at all,
// because the engine's own gate now reads the rule.
func TestAppARealAlwaysIsHonouredOnTheNextCall(t *testing.T) {
	root := resolvedTempDir(t)
	_, workspace := wakeEnvironment(t, root)
	model := consentSource(t, "printf app-always-probe")
	t.Cleanup(model.Close)
	t.Setenv("CODEAF_BASE_URL", model.URL)
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "config.json"), []byte(`{"tools.approvalMode":"prompt"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := newAppRecorder()
	backend := newAppBackend(rec, appOpenInProcess)
	defer backend.Close()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatalf("create = %v", err)
	}
	first, err := backend.Send(agent.ID, "run the probe", "")
	if err != nil {
		t.Fatal(err)
	}
	i := rec.waitFor(t, 30*time.Second, appAskEvent(appwire.EventAskOpened))
	rec.mu.Lock()
	ask := rec.data[i].(appwire.AskOpened).Ask
	rec.mu.Unlock()
	if ask.Subkind != appwire.SubkindPermission || len(ask.Options) != 3 || ask.Options[2].ID != appwire.OptionAlways {
		t.Fatalf("ask = %+v", ask)
	}
	if err := backend.AnswerAsk(agent.ID, ask.ID, appwire.OptionAlways, ""); err != nil {
		t.Fatalf("answer = %v", err)
	}
	rec.waitFor(t, 30*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AskClosed)
		return ok && c.ID == ask.ID && c.By == appwire.ClosedByApp && c.Option == appwire.OptionAlways
	})
	rec.waitFor(t, 30*time.Second, func(event string, data any) bool {
		d, ok := data.(appwire.TurnDone)
		return ok && d.Turn == first && d.OK
	})
	if rules := config.BashApprovalsAt(config.ProfileDir()); !strings.Contains(rules, "printf app-always-probe") {
		t.Fatalf("the profile's shell rules = %s", rules)
	}
	rec.waitFor(t, 30*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.State == appwire.StateIdle
	})

	second, err := backend.Send(agent.ID, "run it again", "")
	if err != nil {
		t.Fatal(err)
	}
	d := rec.waitFor(t, 30*time.Second, func(event string, data any) bool {
		done, ok := data.(appwire.TurnDone)
		return ok && done.Turn == second
	})
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if done := rec.data[d].(appwire.TurnDone); !done.OK {
		t.Fatalf("second turn = %+v", done)
	}
	asked, ran := 0, false
	for j := i + 1; j <= d; j++ {
		if _, ok := rec.data[j].(appwire.AskOpened); ok {
			asked++
		}
		if tool, ok := rec.data[j].(appwire.TurnTool); ok && tool.Turn == second && tool.Phase == appwire.PhaseEnd && tool.Name == "bash" && tool.OK != nil && *tool.OK {
			ran = true
		}
	}
	if asked != 0 || !ran {
		t.Fatalf("after always: asked %d more times, the call ran = %v", asked, ran)
	}
}

// AN APP AGENT KEEPS THE NAME ITS FIRST MESSAGE GAVE IT. A real conversation
// created with words is named by them at once, the engine's own naming does
// not rename it once the turn is answered, and the disk says the same name
// after the conversation is let go.
func TestAppARealAgentKeepsItsFirstMessageName(t *testing.T) {
	root := resolvedTempDir(t)
	_, workspace := wakeEnvironment(t, root)
	rec := newAppRecorder()
	backend := newAppBackend(rec, appOpenInProcess)
	words := "say done and then tell me about the weather in the harbour today"
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace, Text: words})
	if err != nil {
		backend.Close()
		t.Fatalf("create = %v", err)
	}
	want := session.FirstMessageName(words)
	if agent.Title != want {
		backend.Close()
		t.Fatalf("title = %q, want %q", agent.Title, want)
	}
	rec.waitFor(t, 30*time.Second, appTurnDone)
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	for j, event := range rec.events {
		if c, ok := rec.data[j].(appwire.AgentChanged); ok && event == appwire.EventAgentChanged && c.Agent.Title != want {
			rec.mu.Unlock()
			t.Fatalf("renamed to %q", c.Agent.Title)
		}
	}
	rec.mu.Unlock()
	fresh := newAppBackend(newAppRecorder(), nil)
	defer fresh.Close()
	agents, err := fresh.ListAgents(workspace)
	if err != nil || len(agents) != 1 || agents[0].Title != want {
		t.Fatalf("listed = %+v, %v", agents, err)
	}
}

// AGENTS.CREATE ANSWERS AT ONCE. The answer leaves while the conversation is
// still being assembled — here an opener that does not return until it is
// let — with the agent already working and named; the turn starts once the
// conversation is open. A conversation that cannot be opened ends as a failed
// turn and a stopped agent the person can retry.
func TestAppCreateAnswersBeforeTheConversationIsOpen(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
	gate := make(chan struct{})
	var mu sync.Mutex
	var opens, releases int
	inner := fakeOpener(conv, &opens, &releases, &mu)
	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		<-gate
		if !ask.Minted || ask.Model != "qwen/qwen3-coder" {
			t.Errorf("the open was asked %+v", ask)
		}
		return inner(ask)
	})
	defer backend.Close()
	start := time.Now()
	agent, err := backend.CreateAgent(appwire.CreateAsk{Workspace: t.TempDir(), Model: "qwen/qwen3-coder", Text: "fix the parser", ClientID: "c-1"})
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if took > 50*time.Millisecond || agent.State != appwire.StateWorking || agent.Title != "fix the parser" || agent.ID != "5e55a0f1" {
		t.Fatalf("create took %v and answered %+v", took, agent)
	}
	if listed, _ := backend.ListAgents(""); len(listed) != 1 || listed[0].ID != agent.ID {
		t.Fatalf("an agent being assembled is not listed: %+v", listed)
	}
	close(gate)
	i := rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		e, ok := data.(appwire.EntryAdded)
		return ok && e.Entry.Kind == appwire.EntryUser
	})
	rec.mu.Lock()
	user := rec.data[i].(appwire.EntryAdded).Entry
	rec.mu.Unlock()
	if user.ClientID != "c-1" || user.ID != "u0" || user.Text != "fix the parser" {
		t.Fatalf("the person's entry = %+v", user)
	}
	rec.waitFor(t, 5*time.Second, appTurnDone)

	failing := newFakeBackend(rec, func(appOpenAsk) (*appConn, error) { return nil, errors.New("the profile could not be read") })
	defer failing.Close()
	broken, err := failing.CreateAgent(appwire.CreateAsk{Workspace: t.TempDir(), Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		d, ok := data.(appwire.TurnDone)
		return ok && d.Agent == broken.ID && !d.OK && strings.Contains(d.Error, "profile could not be read")
	})
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.ID == broken.ID && c.Agent.State == appwire.StateStopped
	})
}

// The engine's dim notices are quiet entries, and set_effort moves the level.
func TestAppNoticesAreQuietAndEffortMoves(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{
		{Kind: session.EventNotice, Text: "skills carried: skill-installer, find-skills"},
		{Kind: session.EventTurnDone},
	}}
	var mu sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	backend := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Send(agent.ID, "go", ""); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		e, ok := data.(appwire.EntryAdded)
		return ok && e.Entry.Kind == appwire.EntryNotice && e.Entry.Quiet && strings.HasPrefix(e.Entry.Text, "skills carried")
	})
	rec.waitFor(t, 5*time.Second, appTurnDone)
	moved, err := backend.SetEffort(agent.ID, appwire.EffortHigh)
	if err != nil || moved.Effort != appwire.EffortHigh {
		t.Fatalf("set_effort = %+v, %v", moved, err)
	}
}

// appTwoMessageSource is a scripted model whose turn says something, calls one
// shell command, and then says something else: two assistant messages with a
// tool call between them.
func appTwoMessageSource(t *testing.T, command string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var envelope struct {
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
			Tools  []json.RawMessage `json:"tools"`
			Stream bool              `json:"stream"`
		}
		if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		toolTurn := len(envelope.Tools) > 0 && (len(envelope.Messages) == 0 || envelope.Messages[len(envelope.Messages)-1].Role != "tool")
		content := `"content":"All done, the probe printed."`
		finish := "stop"
		if toolTurn {
			arguments, _ := json.Marshal(map[string]string{"command": command})
			content = `"content":"Let me run the probe first.","tool_calls":[{"index":0,"id":"call-probe","type":"function","function":{"name":"bash","arguments":` + strconv.Quote(string(arguments)) + `}}]`
			finish = "tool_calls"
		}
		body := `{"id":"two-messages","choices":[{"index":0,"delta":{"role":"assistant",` + content + `},"finish_reason":"` + finish + `"}]}`
		if envelope.Stream {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = writer.Write([]byte("data: " + body + "\n\ndata: [DONE]\n\n"))
			return
		}
		_, _ = writer.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`))
	}))
}

// ONE MESSAGE, ONE ENTRY. A real turn that says something, runs a tool and
// says something else, recorded event by event: the person's message is one
// user entry carrying the app's client id; each assistant message is exactly
// one answer entry, with the msg its deltas carried; the tool row carries the
// terminal's own line for the call; and agents.entries afterwards names every
// one of those entries by the id it was sent with.
func TestAppATurnWithToolsSaysEachMessageOnce(t *testing.T) {
	root := resolvedTempDir(t)
	_, workspace := wakeEnvironment(t, root)
	model := appTwoMessageSource(t, "printf app-two-messages")
	t.Cleanup(model.Close)
	t.Setenv("CODEAF_BASE_URL", model.URL)
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "config.json"), []byte(`{"tools.approvalMode":"allow"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := newAppRecorder()
	backend := newAppBackend(rec, appOpenInProcess)
	defer backend.Close()
	agent, err := backend.CreateAgent(appwire.CreateAsk{Workspace: workspace, Text: "run the probe", ClientID: "bubble-7"})
	if err != nil {
		t.Fatal(err)
	}
	d := rec.waitFor(t, 30*time.Second, appTurnDone)
	rec.mu.Lock()
	events := append([]string(nil), rec.events[:d+1]...)
	data := append([]any(nil), rec.data[:d+1]...)
	rec.mu.Unlock()
	if done := data[d].(appwire.TurnDone); !done.OK {
		t.Fatalf("turn.done = %+v", done)
	}
	var users, answers []appwire.Entry
	deltas := map[int]string{}
	var title string
	for j, event := range events {
		switch v := data[j].(type) {
		case appwire.EntryAdded:
			switch v.Entry.Kind {
			case appwire.EntryUser:
				users = append(users, v.Entry)
			case appwire.EntryAnswer, appwire.EntryUpdate:
				answers = append(answers, v.Entry)
			}
		case appwire.TurnDelta:
			if event == appwire.EventTurnDelta && v.Kind == appwire.DeltaText {
				deltas[v.Msg] += v.Text
			}
		case appwire.TurnTool:
			if v.Phase == appwire.PhaseBegin {
				title = v.Title
			}
		}
	}
	if len(users) != 1 || users[0].ClientID != "bubble-7" || users[0].Text != "run the probe" {
		t.Fatalf("user entries = %+v", users)
	}
	if len(answers) != 2 || answers[0].Msg == nil || *answers[0].Msg != 0 || answers[1].Msg == nil || *answers[1].Msg != 1 {
		t.Fatalf("answer entries = %+v", answers)
	}
	if answers[0].Text != "Let me run the probe first." || answers[1].Text != "All done, the probe printed." {
		t.Fatalf("answers = %q, %q", answers[0].Text, answers[1].Text)
	}
	if strings.TrimSpace(deltas[0]) != answers[0].Text || strings.TrimSpace(deltas[1]) != answers[1].Text {
		t.Fatalf("deltas by message = %v", deltas)
	}
	if title != "bash printf app-two-messages" {
		t.Fatalf("the tool's title = %q", title)
	}
	listed, _, err := backend.Entries(agent.ID, "", appwire.MaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]appwire.Entry{}
	for _, e := range listed {
		ids[e.ID] = e
	}
	for _, live := range append(users, answers...) {
		if back, ok := ids[live.ID]; !ok || back.Kind != live.Kind || back.Text != live.Text {
			t.Fatalf("live entry %s (%s %q) reads back as %+v; listed %+v", live.ID, live.Kind, live.Text, back, listed)
		}
	}
	if tool, ok := ids["tcall-probe"]; !ok || tool.Title != "bash printf app-two-messages" {
		t.Fatalf("the tool entry = %+v; listed %+v", tool, listed)
	}
}

// EVERY TASK, NESTED ONES INCLUDED, IS SAID AS IT MOVES. A proposal is a queued
// task; its first update moves it under the same id; a sub-task names its
// parent and an adaptive run's rows name their group.
func TestAppSaysNestedTasksLive(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{
		{Kind: session.EventTaskProposal, Task: &session.TaskNotice{ID: 1, Title: "outer"}},
		{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 1, Title: "outer", State: session.TaskRunning}},
		{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 2, Title: "middle", Parent: 1, State: session.TaskQueued}},
		{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 3, Title: "inner", Parent: 2, Run: "run-9", State: session.TaskRunning}},
		{Kind: session.EventTurnDone},
	}}
	var mu sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	backend := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Send(agent.ID, "make three nested tasks", ""); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, appTurnDone)
	var changes []appwire.Task
	rec.mu.Lock()
	for _, data := range rec.data {
		if c, ok := data.(appwire.TaskChanged); ok {
			changes = append(changes, c.Task)
		}
	}
	rec.mu.Unlock()
	if len(changes) != 4 || changes[0].State != appwire.TaskQueued || changes[1].State != appwire.TaskRunning || changes[1].ID != agent.ID+":1" || !changes[0].Proposed {
		t.Fatalf("task.changed = %+v", changes)
	}
	if changes[2].Parent != agent.ID+":1" || changes[3].Parent != agent.ID+":2" || changes[3].Group != "run-9" {
		t.Fatalf("nested = %+v", changes)
	}
	tasks, err := backend.ListTasks(agent.ID)
	if err != nil || len(tasks) != 3 {
		t.Fatalf("tasks.list = %+v, %v", tasks, err)
	}
}
