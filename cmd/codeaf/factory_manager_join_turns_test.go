package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/factorytest"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── WHOLE TURNS OF THE JOB'S MAIN AGENT, INTO THE REAL SERVICE ──────────────
//
// The tests beside this file (factory_manager_join_test.go) press the
// manager's door as a tool does. These go one step further back: the job's
// conversation is a real session, a turn is a real turn of it, and the model
// is scripted to call `factory_status`, `factory_act`, `factory_task` and
// `factory_answer` with the JSON arguments a model writes. So the arguments,
// the person's own words of the turn, when the turn began, the door, the
// app's press and the service are each the real one, and what the model reads
// back is read here from the request that carries it.

// joinCall is one tool call the scripted model makes: the tool's name and its
// arguments. from, when set, writes the arguments from what the model has
// read back so far in the turn, as a model that reads a status and then acts
// on what it says does.
type joinCall struct {
	tool string
	args map[string]any
	from func(read []string) map[string]any
}

// joinModel is the scripted model behind the conversation: a provider that
// answers each request of a turn with the next call it was given, then with a
// last word, and keeps every tool result it was sent.
type joinModel struct {
	mu      sync.Mutex
	calls   []joinCall
	results []string
	asked   int
	server  *httptest.Server
}

func newJoinModel(t *testing.T) *joinModel {
	m := &joinModel{}
	m.server = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.server.Close)
	return m
}

func (m *joinModel) serve(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	say := func(delta, finish string) {
		fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":%s,\"finish_reason\":%s}]}\n\n", delta, finish)
	}
	defer fmt.Fprint(w, "data: [DONE]\n\n")
	// An errand beside the turn (a title, a caption) carries no tools.
	if len(request.Tools) == 0 {
		say(`{"role":"assistant","content":"ok"}`, `"stop"`)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if n := len(request.Messages); n > 0 && request.Messages[n-1].Role == "tool" {
		var text string
		if json.Unmarshal(request.Messages[n-1].Content, &text) != nil {
			text = string(request.Messages[n-1].Content)
		}
		m.results = append(m.results, text)
	}
	if len(m.calls) == 0 {
		say(`{"role":"assistant","content":"Done."}`, `"stop"`)
		return
	}
	call := m.calls[0]
	m.calls = m.calls[1:]
	m.asked++
	if call.from != nil {
		call.args = call.from(m.results)
	}
	args, _ := json.Marshal(call.args)
	function, _ := json.Marshal(map[string]string{"name": call.tool, "arguments": string(args)})
	say(fmt.Sprintf(`{"role":"assistant","tool_calls":[{"index":0,"id":"call-%d","type":"function","function":%s}]}`, m.asked, function), "null")
	say(`{}`, `"tool_calls"`)
}

// joinTalk is the job's own conversation: a real session whose `factory_*`
// tools press the rig's door.
type joinTalk struct {
	j     *joinRig
	agent *session.Agent
	model *joinModel
}

// converse opens the conversation that manages the rig's job.
func (j *joinRig) converse() *joinTalk {
	j.t.Helper()
	model := newJoinModel(j.t)
	agent, err := session.New(session.Config{Workspace: j.t.TempDir(), Model: "test/model", APIKey: "fixture", BaseURL: model.server.URL,
		OneModel: true, DeterministicNames: true, PromptProfile: "full", SessionFile: j.talk, FactoryRun: j.door.(session.RunDoor)})
	if err != nil {
		j.t.Fatal(err)
	}
	j.t.Cleanup(func() { _ = agent.Close() })
	return &joinTalk{j: j, agent: agent, model: model}
}

// turn runs one turn in which the model makes calls, in order, and answers
// what it read back from each. submit begins the turn.
func (c *joinTalk) turn(submit func(context.Context) (<-chan session.Event, error), calls []joinCall) []string {
	c.model.mu.Lock()
	c.model.calls, c.model.results = append([]joinCall(nil), calls...), nil
	c.model.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	events, err := submit(ctx)
	if err != nil {
		c.j.t.Errorf("the turn did not begin: %v", err)
		return nil
	}
	for ev := range events {
		if ev.Kind == session.EventError {
			c.j.t.Errorf("the turn failed: %v", ev.Err)
		}
	}
	c.model.mu.Lock()
	defer c.model.mu.Unlock()
	if len(c.model.calls) != 0 || len(c.model.results) != len(calls) {
		c.j.t.Errorf("the model made %d of its %d calls and read %d results", len(calls)-len(c.model.calls), len(calls), len(c.model.results))
	}
	return append(make([]string, 0, len(calls)), c.model.results...)
}

// person is a turn the person begins by typing words.
func (c *joinTalk) person(words string, calls ...joinCall) []string {
	return c.turn(func(ctx context.Context) (<-chan session.Event, error) { return c.agent.Submit(ctx, words) }, calls)
}

// engine is a turn the factory begins with a note of its own: a stop, a
// stall. It carries no word of the person's.
func (c *joinTalk) engine(note string, calls ...joinCall) []string {
	return c.turn(func(ctx context.Context) (<-chan session.Event, error) { return c.agent.SubmitRunnerNote(ctx, note) }, calls)
}

func joinAnswer(action, answer string) joinCall {
	args := map[string]any{"action": action}
	if answer != "" {
		args["answer"] = answer
	}
	return joinCall{tool: "factory_answer", args: args}
}

// onOneClock dates the acts the job stores by the wall clock. A scenario's
// clock runs ahead of the wall clock, because it skips Undo windows instead
// of sitting them out, while a real turn begins by the wall clock. In the app
// the service and the turn read one clock. The rule that compares the two
// (an act proposed in this same turn) is given like for like here.
func (j *joinRig) onOneClock() {
	ahead := j.s.Now().Sub(time.Now())
	j.s.Change(func(it *factory.Item) {
		for i := range it.Acts {
			it.Acts[i].At = it.Acts[i].At.Add(-ahead)
		}
	})
}

// AUTO, A WHOLE TURN. The person asks; the model reads `factory_status`,
// which lists the review thread by the id `factory_act` takes, and answers
// it. The reply is posted once as written, the thread is resolved, the
// reviewer is asked again, and a `factory_task` then runs as a Follow PR
// round with the instructions the model wrote.
func TestManagerTurnInAutoReachesGitHub(t *testing.T) {
	j, thread, _ := joinReviewed(t, factory.AutonomyAuto)
	c := j.converse()
	var read []string
	it := j.s.Do("the person asks the main agent to answer the review", func() {
		read = c.person("answer bob, resolve his thread and ask him to look again",
			joinCall{tool: "factory_status", args: map[string]any{}},
			joinCall{tool: "factory_act", args: map[string]any{"why": "  bob's rename  is in ", "acts": []map[string]any{
				{"kind": "reply", "thread": " " + thread + " ", "text": "Renamed it to settled.\n\nThanks for the catch."},
				{"kind": "resolve_thread", "thread": thread},
				{"kind": "rerequest_review", "reviewers": []string{" @bob "}},
			}}})
		j.s.ActsSent()
	})
	if len(read) != 2 {
		t.Fatalf("the model read %q\n%s", read, j.s.Story())
	}
	if !strings.Contains(read[0], "- "+thread+" · by @bob") || !strings.Contains(read[0], "Autonomy: Auto") {
		t.Errorf("factory_status does not list the thread by the id an act takes:\n%s", read[0])
	}
	if !strings.Contains(read[1], "Accepted: 3 public acts (reply, resolve_thread, rerequest_review)") {
		t.Errorf("factory_act read back %q", read[1])
	}
	want := forge.PublicationMarker + "\nRenamed it to settled.\n\nThanks for the catch.\n\n" + factory.PublicActFooter
	if posted := j.s.GitHub.Posted(); len(posted) != 1 || posted[0] != want {
		t.Fatalf("posted %q, want %q once\n%s", posted, want, j.s.Story())
	}
	if !j.s.GitHub.Resolved(thread) || writesOf(j.s, "rerequest bob") != 1 {
		t.Fatalf("the thread was not resolved or the review not asked again: %v\n%s", j.s.GitHub.Writes(), j.s.Story())
	}
	for _, act := range it.Acts {
		if act.State != factory.ActDone || act.Why != "bob's rename is in" || act.ApprovedBy != factory.ActByMainAgent {
			t.Fatalf("the service stored %+v\n%s", act, j.s.Story())
		}
	}
	rounds := j.s.Delegate.Rounds(factory.FollowPRStep)
	j.s.Do("the person asks for one more change, and the main agent sends a task", func() {
		read = c.person("also note the rounding rule in the README", joinCall{tool: "factory_task", args: map[string]any{"instructions": "  " + joinTaskWords + "\n", "now": false}})
		j.s.Poll()
	})
	if len(read) != 1 || !strings.Contains(read[0], "Task taken") {
		t.Errorf("factory_task read back %q", read)
	}
	if j.s.Delegate.Rounds(factory.FollowPRStep) != rounds+1 || !j.s.Delegate.Heard(factory.FollowPRStep, 0, joinTaskWords) {
		t.Fatalf("the task did not run as a Follow PR round with its instructions: %s\n%s", j.s.Delegate.Sequence(), j.s.Story())
	}
}

// MANUAL, WHOLE TURNS. What the model writes in its arguments never stands in
// for the person: on a turn the factory began, approve, approve_acts and merge
// change nothing whatever `answer` says, and the model reads why in words. On
// the person's own turns the same calls do what they say, except approving
// acts in the very turn that proposed them.
func TestManagerTurnsInManualHoldTheAuthorityTable(t *testing.T) {
	j, thread, it := joinReviewed(t, factory.AutonomyManual)
	if it.Wait.Action != factory.ActApprovePublish {
		t.Fatalf("Manual did not wait to push the repair: %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
	c := j.converse()
	pushes := writesOf(j.s, "push ")
	// A TURN THE FACTORY BEGAN: the model claims the person's word, and has none.
	read := c.engine("This job has not moved for a while. Decide what to do.",
		joinAnswer("approve", "the person told me to approve publishing"),
		joinAnswer("merge", "the person told me to merge it now"),
	)
	if len(read) != 2 || !strings.HasPrefix(read[0], "Nothing changed.") || !strings.Contains(read[0], "approving publication is the person's, and nothing in this turn came from them") {
		t.Fatalf("approve on the factory's own turn read back %q", read)
	}
	if !strings.HasPrefix(read[1], "Nothing changed.") || !strings.Contains(read[1], "merging is the person's alone") {
		t.Fatalf("merge on the factory's own turn read back %q", read[1])
	}
	if it = j.s.Do("time passes", j.s.Tick); it.Wait.Action != factory.ActApprovePublish || writesOf(j.s, "push ") != pushes || it.Merging != nil {
		t.Fatalf("the factory's own turn moved the job: %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
	// THE PERSON'S TURN.
	it = j.s.Do("the person says to push, and the main agent approves", func() {
		read = c.person("yes, push the fix", joinAnswer("approve", "yes, push the fix"))
	})
	if len(read) != 1 || !strings.Contains(read[0], "Publication approved, as the person") || it.State != factory.StateShipped || writesOf(j.s, "push ") != pushes+1 {
		t.Fatalf("the person's approval: read %q, the job %s\n%s", read, factorytest.WaitKey(it), j.s.Story())
	}
	// PROPOSED AND APPROVED IN ONE BREATH is refused: the person has not read
	// what the acts say.
	reply := joinCall{tool: "factory_act", args: map[string]any{"why": "bob's rename is in", "acts": []map[string]any{{"kind": "reply", "thread": thread, "text": "Renamed it to settled."}}}}
	it = j.s.Do("the person asks for a reply, and the main agent proposes it and tries to approve it", func() {
		read = c.person("answer bob's comment", reply, joinAnswer("approve_acts", ""))
		j.s.ActsSent()
	})
	if len(read) != 2 || !strings.Contains(read[0], "Proposed: 1 public act (reply)") || !strings.HasPrefix(read[1], "Nothing changed.") || !strings.Contains(read[1], "you proposed these in this same turn") {
		t.Fatalf("propose and approve in one turn read back %q\n%s", read, j.s.Story())
	}
	if it.Wait.Action != factory.ActApproveActs || len(j.s.GitHub.Posted()) != 0 {
		t.Fatalf("the reply did not wait for the person: %s, posted %q\n%s", factorytest.WaitKey(it), j.s.GitHub.Posted(), j.s.Story())
	}
	j.onOneClock()
	// The factory's own turn cannot approve them either.
	read = c.engine("The acts still wait.", joinAnswer("approve_acts", "the person said yes"))
	if len(read) != 1 || !strings.Contains(read[0], "approving public acts is the person's") || len(j.s.GitHub.Posted()) != 0 {
		t.Fatalf("approve_acts on the factory's own turn read back %q, posted %q", read, j.s.GitHub.Posted())
	}
	it = j.s.Do("the person reads the reply and says yes", func() {
		read = c.person("yes, post it", joinAnswer("approve_acts", ""))
		j.s.ActsSent()
	})
	if len(read) != 1 || !strings.Contains(read[0], "approved, as the person") {
		t.Fatalf("the person's yes read back %q\n%s", read, j.s.Story())
	}
	if posted := j.s.GitHub.Posted(); len(posted) != 1 || posted[0] != forge.PublicationMarker+"\nRenamed it to settled.\n\n"+factory.PublicActFooter || actOf(t, it, factory.ActKindReply).ApprovedBy != "me" {
		t.Fatalf("posted %q, acts %+v\n%s", posted, it.Acts, j.s.Story())
	}
	// DECLINING IS THE MANAGER'S, and its reason is kept on the act.
	comment := joinCall{tool: "factory_act", args: map[string]any{"why": "a nudge", "acts": []map[string]any{{"kind": "comment", "text": "Ready for another look."}}}}
	it = j.s.Do("the main agent proposes a comment and then declines it", func() {
		read = c.engine("Bob approved the change meanwhile.", comment, joinAnswer("decline_acts", "Bob approved it already."))
		j.s.ActsSent()
	})
	if act := actOf(t, it, factory.ActKindComment); len(read) != 2 || act.State != factory.ActDeclined || act.Result != "Bob approved it already." || len(j.s.GitHub.Posted()) != 1 {
		t.Fatalf("the declined comment: read %q, stored %+v\n%s", read, act, j.s.Story())
	}
}

// A STOP, DECIDED BY A WHOLE TURN. The service hands a step out of rounds to
// the main agent with the app's own words for it ([stopAskFor]); the turn is
// the factory's, and the model answers with `factory_answer` another_round.
// The step runs again with the model's instructions and the person is never
// asked.
func TestManagerTurnOnAStopGivesAnotherRound(t *testing.T) {
	j := newJoin(t, factory.AutonomyManual, "write", "test", "proof")
	c := j.converse()
	j.s.Delegate.Next("test", factorytest.Round{Result: joinNotGreen()}, factorytest.Round{Result: joinNotGreen()}, factorytest.Round{Result: joinNotGreen()})
	const words = "Run the ledger tests with the race detector on."
	var mu sync.Mutex
	var asks, read []string
	j.s.StopAgent(func(it factory.Item, wait factory.Wait) (string, error) {
		ask := stopAskFor(it, wait)
		got := c.engine(ask, joinCall{tool: "factory_answer", args: map[string]any{"action": "another_round", "step": "test", "answer": words}})
		mu.Lock()
		asks, read = append(asks, ask), append(read, got...)
		mu.Unlock()
		return "Gave test another round.", nil
	})
	it := j.sayYesUntil(func(it factory.Item) bool { return j.s.Delegate.Rounds("proof") > 0 })
	mu.Lock()
	defer mu.Unlock()
	if len(asks) != 1 || !strings.Contains(asks[0], "action another_round") || !strings.Contains(asks[0], "Accepted from you now: task, another_round, send_back, rerun, accept, hand_to_person.") {
		t.Fatalf("the hand-over: %q\n%s", asks, j.s.Story())
	}
	if len(read) != 1 || !strings.Contains(read[0], "The step gets another round with your instructions.") {
		t.Fatalf("factory_answer read back %q\n%s", read, j.s.Story())
	}
	if j.s.Delegate.Rounds("test") != 4 || !j.s.Delegate.Heard("test", 4, words) || !joinLogged(it, "Main agent: another round · "+words) {
		t.Fatalf("test ran %d rounds\n%s", j.s.Delegate.Rounds("test"), j.s.Story())
	}
	for _, e := range j.s.Events {
		if strings.HasSuffix(e, "-> you:answer") {
			t.Fatalf("the person was asked about a stop the main agent settled: %s\n%s", e, j.s.Story())
		}
	}
}

// joinQuestionID reads the pending question's id out of a `factory_status`
// answer, as the model does before it answers.
var joinQuestionID = regexp.MustCompile(`Pending question ID: (\S+)\.`)

// A STEP'S QUESTION NOBODY ANSWERED IS THE MAIN AGENT'S STOP TOO, AND A WHOLE
// TURN ANSWERS IT. The step asked, the main agent's turn on the question
// never came, and the service hands the stop over. The model reads
// `factory_status` for the question's id and answers it with
// `factory_answer`; the step goes on with the answer, and the person is
// never asked.
func TestManagerTurnOnAStepsQuestionAnswersIt(t *testing.T) {
	j := newJoin(t, factory.AutonomyAuto, "write", "proof")
	c := j.converse()
	j.s.Delegate.Next("write", factorytest.Round{Ask: "Rename the public constant too?"})
	// The runner's own turn on the question answers nothing and says nothing.
	j.s.MainAgent(func(factory.Asked) (string, string) { return "", "" })
	var mu sync.Mutex
	var kinds, read []string
	j.s.StopAgent(func(it factory.Item, wait factory.Wait) (string, error) {
		got := c.engine(stopAskFor(it, wait),
			joinCall{tool: "factory_status", args: map[string]any{}},
			joinCall{tool: "factory_answer", from: func(read []string) map[string]any {
				id := ""
				if m := joinQuestionID.FindStringSubmatch(read[0]); m != nil {
					id = m[1]
				}
				return map[string]any{"question_id": id, "answer": "Yes, rename the public constant too."}
			}})
		mu.Lock()
		kinds, read = append(kinds, factory.ManagerStop(it)), append(read, got...)
		mu.Unlock()
		return "Answered the step.", nil
	})
	it := j.s.Do("start, and write asks a question nobody answers", func() { j.s.MustPress(factory.DoStart) })
	mu.Lock()
	defer mu.Unlock()
	if len(kinds) != 1 || kinds[0] != factory.StopQuestion || len(read) != 2 {
		t.Fatalf("the main agent's turns: %v, and it read %q\n%s", kinds, read, j.s.Story())
	}
	if !strings.Contains(read[0], "write asks: Rename the public constant too?") || !joinQuestionID.MatchString(read[0]) {
		t.Fatalf("factory_status does not carry the question and its id:\n%s", read[0])
	}
	if strings.HasPrefix(read[1], "Nothing changed") || strings.HasPrefix(read[1], "Action not confirmed") {
		t.Fatalf("factory_answer read back %q\n%s", read[1], j.s.Story())
	}
	if it.PullRequest == nil || it.Asking != nil || j.s.Delegate.Rounds("proof") != 1 {
		t.Fatalf("the answered step did not carry the job on: %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
	for _, e := range j.s.Events {
		if strings.Contains(e, "-> you:") {
			t.Fatalf("the person was asked: %s\n%s", e, j.s.Story())
		}
	}
}
