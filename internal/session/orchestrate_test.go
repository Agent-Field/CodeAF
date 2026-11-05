package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/orchestrate"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// THE ADAPTIVE RUN from the four sides a person meets it: the sentence that
// asks for one, the turn it must not hold, the run itself, and the bound a
// node works inside.

// runConfig is the most permissive build there is on this side: a runner wired
// to launch a run, and somebody watching who could have answered its fuel gate.
// Those were the two gates the old cue asked about, so a build that passes both
// is the one where a door, if there were one, would certainly be open.
func runConfig(started *startedRun) func(*Config) {
	return func(config *Config) {
		config.AskConsent = true
		config.OrchestrateRunner = started.launch
	}
}

// startedRun records anything that reaches the runner. Nothing in a conversation
// does any more, which is what the tests below are for.
type startedRun struct {
	goal  string
	model string
	cap   float64
	calls int
}

func (s *startedRun) launch(_ context.Context, goal, model string, capDollars float64) (string, error) {
	s.calls++
	s.goal, s.model, s.cap = goal, model, capDollars
	return "7", nil
}

// ── the door that is not there ──────────────────────────────────────────────

// NO SENTENCE OPENS A RUN. A message beginning `orchestrate …` was the last way
// a conversation could reach the planner, and it is gone: those words are an
// ordinary turn now, answered by the model like any other, and the work they ask
// for goes out on the one road everything else takes.
//
// This is ABSENCE AND NOT REFUSAL, so what the test asserts is that the turn is
// UNREMARKABLE — the model was asked, it answered, the turn ended — and that the
// runner, wired and watched and as ready as a build can be, was never called.
func TestTheWordsThatOnceOpenedARunAreAnOrdinaryTurn(t *testing.T) {
	const answer = "the old client is used in four places; here is what a migration touches"
	for _, typed := range []string{
		"orchestrate the migration off the old client",
		"orchestrate the migration off the old client with a $5 budget",
		"please orchestrate the audit with opus",
		"adaptively work on the release notes",
		"run an adaptive run on the flaky test suite",
		"start adaptive run: rewrite the docs",
	} {
		var started startedRun
		completer := &scriptedCompleter{steps: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse(answer), nil
			},
		}}
		agent, _ := newTestAgent(t, completer, runConfig(&started))

		events, err := agent.Submit(context.Background(), typed)
		if err != nil {
			t.Fatalf("submit %q: %v", typed, err)
		}
		collected := collect(t, events)

		if started.calls != 0 {
			t.Fatalf("%q started a run: %+v", typed, started)
		}
		if completer.requests() == 0 {
			t.Fatalf("%q never reached the model", typed)
		}
		if _, ok := firstOfKind(collected, EventTurnDone); !ok {
			t.Fatalf("%q did not finish as a turn: %v", typed, kinds(collected))
		}
		if said := lastSaid(agent); !strings.Contains(said, answer) {
			t.Fatalf("%q was answered with %q", typed, said)
		}
	}
}

// AND THE SENTENCE IS NOT SPECIAL-CASED ANYWHERE ELSE EITHER: the words carry no
// note, no refusal and no offer, so the goal reaches the model exactly as typed.
func TestTheOldCueReachesTheModelWordForWord(t *testing.T) {
	const typed = "orchestrate the migration off the old client with a $5 budget"
	var started startedRun
	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, runConfig(&started))

	events, err := agent.Submit(context.Background(), typed)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)

	if completer.requests() == 0 {
		t.Fatal("the turn was answered without asking the model anything")
	}
	sent := completer.request(0)
	if len(sent) == 0 || !strings.Contains(lastUserText(sent), typed) {
		t.Fatalf("the model was sent %q, want the sentence as typed", lastUserText(sent))
	}
	if started.calls != 0 {
		t.Fatalf("a run started anyway: %+v", started)
	}
}

// ── the seams ───────────────────────────────────────────────────────────────

func TestTheRunSeamsRefuseAnIdNobodyMinted(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if _, ok := agent.OrchestrateSnapshot("9"); ok {
		t.Fatalf("a run nobody started has no snapshot")
	}
	if _, err := agent.ResolveOrchestrate("9", "finish"); err == nil {
		t.Fatalf("there is nothing to answer")
	}
	if err := agent.SteerOrchestrate("9", "do the other one"); err == nil {
		t.Fatalf("there is nothing to steer")
	}
}

// ── the run ─────────────────────────────────────────────────────────────────

// replier is a completer that answers what it was ASKED rather than what step
// it is on: a run has several agents talking at once, and an index-ordered
// script cannot tell them apart.
type replier func(messages []ai.Message) string

func (r replier) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	return textResponse(r(messages)), nil
}

// truncationReplier reproduces the failed writer-node shape: the planner makes
// one node, then that node repeatedly emits prose whose provider stop says it
// was cut off before the intended tool call could materialize.
type truncationReplier struct {
	mu        sync.Mutex
	nodeCalls int
	seenNote  bool
}

func (r *truncationReplier) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	asked := lastUserText(messages)
	if isPlannerCall(messages) {
		if strings.Contains(asked, "nothing yet") {
			return textResponse(`{"add":[{"id":"n1","goal":"write the final report","write_scope":["report.md"]}]}`), nil
		}
		return textResponse(`{"done":{"brief":"report the incomplete writer node honestly"}}`), nil
	}
	if strings.Contains(asked, "WHAT THE WORK BEFORE YOU FOUND") {
		return textResponse("The writer node ended at the output limit and did not finish."), nil
	}
	if strings.HasPrefix(asked, "First message:") {
		return textResponse(`{"work":false,"why":"the node was already work"}`), nil
	}
	// THE NAMER IS NOT THE WRITER. This planner adds a node called `n1`, which is
	// filing rather than a name, so the run buys three words for its row from the
	// small namer (taskname.go) — a call that is nobody's turn and must not be
	// counted as one of the writer's.
	if isNameCall(messages) {
		return textResponse("final report"), nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodeCalls++
	if strings.Contains(asked, "cut off at the output limit") {
		r.seenNote = true
	}
	response := textResponse("Now I have the raw files. Let me write the report.")
	response.Choices[0].FinishReason = "length"
	return response, nil
}

func TestAdaptiveNodeContinuesCutOffTextAndStopsHonestlyAtTheBound(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	completer := &truncationReplier{}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.AskConsent = true })

	id, err := agent.RunOrchestrate(context.Background(), "write the report", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	snap := waitForRun(t, agent, id)
	if completer.nodeCalls != 1+truncationContinuations {
		t.Fatalf("writer calls = %d, want initial plus %d bounded continuations", completer.nodeCalls, truncationContinuations)
	}
	if !completer.seenNote {
		t.Fatal("the writer never saw the note explaining that its reply was cut off")
	}
	if len(snap.Nodes) != 1 || !strings.Contains(snap.Nodes[0].Digest, "INCOMPLETE: the node's final reply was cut off at the output limit") {
		t.Fatalf("the node digest hid the truncation: %+v", snap.Nodes)
	}
}

// THE WHOLE RUN, from the goal to the write-up: the planner adds a node, the
// node runs as a child agent in this package's own loop, its digest reaches
// the planner, and the planner's done plan becomes the synthesis.
func TestARunPlansExecutesAndSynthesizes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	completer := replier(func(messages []ai.Message) string {
		asked := lastUserText(messages)
		switch {
		case isPlannerCall(messages):
			if strings.Contains(asked, "nothing yet") {
				return `{"add":[{"id":"n1","goal":"read the release notes"}],"note":"one node to start"}`
			}
			if !strings.Contains(asked, "n1: n1 read the notes") {
				return `{}`
			}
			return `{"done":{"brief":"say what the notes said"}}`
		case strings.Contains(asked, "Ground every claim"):
			return "the write-up, grounded in (n1)"
		default:
			return "n1 read the notes"
		}
	})
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.AskConsent = true })
	lane := agent.Orchestrations()

	id, err := agent.RunOrchestrate(context.Background(), "summarise the release notes", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	snap := waitForRun(t, agent, id)
	if snap.Answer != "the write-up, grounded in (n1)" {
		t.Fatalf("the synthesis is the run's answer, got %q", snap.Answer)
	}
	if len(snap.Nodes) != 1 || snap.Nodes[0].State != orchestrate.Done {
		t.Fatalf("the frontier ended as %+v", snap.Nodes)
	}
	if !strings.Contains(snap.Nodes[0].Digest, "n1 read the notes") {
		t.Fatalf("the node's digest is %q", snap.Nodes[0].Digest)
	}
	if len(snap.Notes) == 0 || snap.Notes[0] != "one node to start" {
		t.Fatalf("the planner's note never landed: %+v", snap.Notes)
	}
	// The planner's note reached whoever was watching, and so did the run's
	// last word.
	var notes []string
	for len(notes) < 2 {
		event := nextRunEvent(t, lane)
		if event.Kind == EventOrchestrateNote {
			notes = append(notes, event.Text)
		}
	}
	if notes[0] != "one node to start" {
		t.Fatalf("the lane carried %q first", notes[0])
	}

	// A FINISHED RUN IS STILL READABLE. What a person opens the room for
	// afterwards is exactly this snapshot, so the run outlives its goroutine.
	if _, known := agent.OrchestrateSnapshot(id); !known {
		t.Fatalf("the finished run was forgotten with its write-up in it")
	}
	if _, err := agent.ResolveOrchestrate(id, "topup:1"); err == nil {
		t.Fatalf("a finished run is not at the gate")
	}
}

// openingPlannerReplier keeps syntax repair separate from planning so a repair
// that discards the nodes cannot masquerade as a successful opening.
type openingPlannerReplier struct {
	mu                            sync.Mutex
	first, second, repair, finish string
	secondFinish                  string
	outputTokens                  int
	repairFinish                  string
	repairs                       int
	openings                      int
	budgets                       []int
}

func (r *openingPlannerReplier) CompleteWithMessages(_ context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if isPlannerCall(messages) {
		opening := false
		for _, message := range messages {
			if strings.Contains(messageText(message), "nothing yet") {
				opening = true
			}
		}
		if !opening {
			return textResponse(`{"done":{"brief":"report the findings"}}`), nil
		}
		r.openings++
		request := &ai.Request{}
		for _, option := range options {
			if err := option(request); err != nil {
				return nil, err
			}
		}
		budget := 0
		if request.MaxTokens != nil {
			budget = *request.MaxTokens
		}
		r.budgets = append(r.budgets, budget)
		raw, finish := r.first, r.finish
		if r.openings > 1 {
			raw, finish = r.second, "stop"
			if r.secondFinish != "" {
				finish = r.secondFinish
			}
		}
		response := textResponse(raw)
		response.Choices[0].FinishReason = finish
		tokens := r.outputTokens
		if tokens == 0 {
			tokens = 2048
		}
		response.Usage = &ai.Usage{CompletionTokens: tokens}
		return response, nil
	}
	if strings.Contains(messageText(messages[0]), "You repair malformed JSON") {
		r.repairs++
		response := textResponse(r.repair)
		if r.repairFinish != "" {
			response.Choices[0].FinishReason = r.repairFinish
		}
		return response, nil
	}
	return textResponse("read the notes"), nil
}

func TestOpeningPlannerRejectsLostReplies(t *testing.T) {
	const plan = `{"add":[{"id":"read-notes","title":"read notes","goal":"read the release notes"}]}`
	for _, tc := range []struct {
		name, first, finish, repair, second string
		proceeds                            bool
		calls                               int
	}{
		{"cut-off twice", `{"add":[{"id":"n1","goal":"read`, "length", "{}", `{"add":[`, false, 2},
		{"cut-off then complete", `{"add":[`, "length", "{}", plan, true, 2},
		{"large cut-off then complete", `{"add":[`, "length", "{}", plan, true, 2},
		{"cut-off empty JSON", "{}", "length", "{}", "{}", false, 2},
		{"cut-off valid JSON", plan, "length", "{}", plan, true, 2},
		{"retry stays malformed", `{"add":[`, "length", "[", `{"add":[`, false, 2},
		{"blank twice", "", "stop", "", "", false, 2},
		{"blank then complete", "", "stop", "", plan, true, 2},
		{"repair loses nodes", `{"add":[{"id":"n1",`, "stop", "{}", "", false, 2},
		{"repair becomes blank", `{"add":[`, "stop", "", "", false, 2},
		{"repair becomes null", `{"add":[`, "stop", "null", "", false, 2},
		{"repair becomes empty add", `{"add":[`, "stop", `{"add":[]}`, "", false, 2},
		{"repair itself cut off", `{"add":[`, "stop", plan, plan, true, 2},
		{"repair retains nodes", `{"add":[{"id":"n1",`, "stop", plan, "", true, 1},
		{"explicit empty", "{}", "stop", "", "", false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			fake := &openingPlannerReplier{first: tc.first, finish: tc.finish, repair: tc.repair, second: tc.second}
			if tc.name == "cut-off twice" {
				fake.secondFinish = "length"
			}
			if tc.name == "large cut-off then complete" {
				fake.outputTokens = 6000
			}
			if tc.name == "repair itself cut off" {
				fake.repairFinish = "length"
			}
			agent, _ := newTestAgent(t, fake, func(config *Config) { config.AskConsent = true })
			updates, stop := agent.WatchTaskUpdates()
			defer stop()
			runNotes, stopRun := agent.WatchOrchestrations()
			defer stopRun()
			id, err := agent.RunOrchestrate(t.Context(), "read the release notes", "", 5)
			if err != nil {
				t.Fatal(err)
			}
			snap := waitForRun(t, agent, id)
			if err := agent.Close(); err != nil {
				t.Fatal(err)
			}
			if fake.openings != tc.calls {
				t.Errorf("opening calls = %d, want %d; snapshot = %+v", fake.openings, tc.calls, snap)
			}
			if fake.budgets[0] != 0 {
				t.Errorf("initial output cap = %d, want provider default", fake.budgets[0])
			}
			if tc.calls == 2 && len(fake.budgets) == 2 {
				want := 8192
				if tc.name == "large cut-off then complete" {
					want = 12000
				}
				if fake.budgets[1] != want {
					t.Errorf("retry budget = %d, want %d", fake.budgets[1], want)
				}
			}
			if tc.finish == "length" && tc.name != "retry stays malformed" && fake.repairs != 0 {
				t.Errorf("cut-off reply reached syntax repair %d times", fake.repairs)
			}
			if tc.proceeds {
				if len(snap.Nodes) != 1 || snap.Nodes[0].State != orchestrate.Done || snap.Answer == "" {
					t.Fatalf("recovered plan did not execute and synthesize: %+v", snap)
				}
				return
			}
			want := "the planner's reply was cut off"
			if tc.name == "explicit empty" {
				want = "The crew didn't plan any work for this goal."
			}
			if snap.EmptyPlanReason() != want {
				t.Errorf("ending = %q, want %q; snapshot = %+v", snap.EmptyPlanReason(), want, snap)
			}
			var notices []string
			var root TaskNotice
		readUpdates:
			for {
				select {
				case event := <-updates:
					if event.Kind == EventNotice {
						notices = append(notices, event.Text)
					}
					if event.Kind == EventTaskUpdate && event.Task != nil && event.Task.Run == id && event.Task.Parent == 0 {
						root = *event.Task
					}
				case <-time.After(20 * time.Millisecond):
					break readUpdates
				}
			}
			if len(notices) != 1 || notices[0] != want {
				t.Errorf("chat notices = %q, want exactly %q", notices, want)
			}
			if root.State != TaskFailed || root.Report != want || !root.Retryable {
				t.Errorf("crew ending: state=%s report=%q retryable=%t", root.State, root.Report, root.Retryable)
			}
			var saved []string
			for _, entry := range agent.Transcript() {
				if entry.Role == "assistant" {
					saved = append(saved, entry.Text)
				}
			}
			if len(saved) != 1 || saved[0] != want {
				t.Errorf("saved notices = %q, want exactly %q", saved, want)
			}
		readRunNotes:
			for {
				select {
				case event := <-runNotes:
					if event.Kind == EventOrchestrateNote {
						t.Errorf("duplicate ending on run lane: %q", event.Text)
					}
				case <-time.After(20 * time.Millisecond):
					break readRunNotes
				}
			}
		})
	}
}

func TestEmptyCrewPlanReportsWhyAndOffersRecovery(t *testing.T) {
	for _, reason := range []string{"", "The goal needs a repository path."} {
		t.Run(reason, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			completer := replier(func(messages []ai.Message) string {
				if isPlannerCall(messages) {
					reply, err := json.Marshal(orchestrate.Amendment{Note: reason})
					if err != nil {
						t.Error(err)
					}
					return string(reply)
				}
				return "repository audit"
			})
			path := filepath.Join(t.TempDir(), "session.jsonl")
			agent, _ := newTestAgent(t, completer, func(config *Config) {
				config.AskConsent = true
				config.SessionFile = path
			})
			runNotes, stopRunNotes := agent.WatchOrchestrations()
			defer stopRunNotes()
			lane, stopNotes := agent.WatchTaskUpdates()
			defer stopNotes()
			updates, stopUpdates := agent.WatchTaskUpdates()
			defer stopUpdates()
			id, err := agent.RunOrchestrate(context.Background(), "audit the repository", "", 5)
			if err != nil {
				t.Fatal(err)
			}
			snap := waitForRun(t, agent, id)
			if len(snap.Nodes) != 0 {
				t.Fatalf("fake planner unexpectedly created workers: %+v", snap.Nodes)
			}
			if err := agent.Close(); err != nil {
				t.Fatal(err)
			}
			want := reason
			if want == "" {
				want = "The crew didn't plan any work for this goal."
			}
			if !snap.Done || snap.Stopped || snap.Answer != "" {
				t.Errorf("empty plan ending = %+v", snap)
			}
			// The ending is an ordinary saved assistant notice, like a budget stop.
			var endings []string
		readNotes:
			for {
				select {
				case event := <-lane:
					if event.Kind == EventNotice {
						endings = append(endings, event.Text)
					}
				case <-time.After(250 * time.Millisecond):
					break readNotes
				}
			}
			if len(endings) != 1 || endings[0] != want {
				t.Errorf("want one exact ending notice %q, got %q", want, endings)
			}
			var saved []string
			for _, entry := range agent.Transcript() {
				if entry.Role == "assistant" {
					saved = append(saved, entry.Text)
				}
			}
			if len(saved) != 1 || saved[0] != want {
				t.Errorf("want one saved assistant line %q, got %q", want, saved)
			}
			var reopened []string
			for _, entry := range ReadTranscript(path).Entries {
				if entry.Role == "assistant" {
					reopened = append(reopened, entry.Text)
				}
			}
			if len(reopened) != 1 || reopened[0] != want {
				t.Errorf("saved journal lost ending: %q", reopened)
			}
		readRunNotes:
			for {
				select {
				case event := <-runNotes:
					if event.Kind == EventOrchestrateNote {
						t.Errorf("duplicate chat line on orchestration lane: %q", event.Text)
					}
				case <-time.After(250 * time.Millisecond):
					break readRunNotes
				}
			}

			var root TaskNotice
			for _, notice := range familyNotices(t, updates) {
				if notice.Run == id && notice.Parent == 0 {
					root = notice
				}
			}
			if root.State != TaskFailed || !root.Retryable || root.Stopped || root.Report != want {
				t.Errorf("empty crew must offer non-success recovery: state=%s retryable=%t", root.State, root.Retryable)
			}
		})
	}
}

func TestEmptyCrewRetryStartsFreshAndChecksAdmission(t *testing.T) {
	for _, via := range []string{"task", "app"} {
		t.Run(via, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			var calls atomic.Int32
			completer := replier(func(messages []ai.Message) string {
				if !isPlannerCall(messages) {
					return "audit"
				}
				if calls.Add(1) == 1 {
					return `{"note":"Need the repository path."}`
				}
				asked := lastUserText(messages)
				if strings.Contains(asked, "Need the repository path.") {
					t.Errorf("retry reused earlier plan output: %s", asked)
				}
				if strings.Contains(asked, "nothing yet") {
					return `{"add":[{"id":"fresh","goal":"read the repository"}]}`
				}
				return `{"done":{"brief":"report the new audit"}}`
			})
			agent, _ := newTestAgent(t, completer, func(c *Config) { c.SpendRailUSD = 5 })
			defer agent.Close()
			id, err := agent.RunOrchestrate(context.Background(), "audit the repository", "", 5)
			if err != nil {
				t.Fatal(err)
			}
			waitForRun(t, agent, id)
			var row TaskNotice
			waitFor(t, "empty-plan root", func() bool {
				for _, r := range agent.appRunRows() {
					if r.Run == id && r.Parent == 0 {
						row = r
					}
				}
				return row.State == TaskFailed
			})
			view, found, err := agent.AppTask(strconv.FormatUint(row.ID, 10))
			if err != nil || !found || !view.Retryable || view.State != "failed" || view.Result != "Need the repository path." {
				t.Fatalf("app ending = %+v, %v", view, err)
			}
			retry := func() error {
				if via == "app" {
					return agent.AppTaskAction(AppTaskIntent{ID: view.ID, Action: "retry"})
				}
				return agent.RetryTask(row.ID)
			}
			spend(agent, 5)
			if err := retry(); !errors.Is(err, ErrSpendRail) {
				t.Fatalf("over-budget retry = %v", err)
			}
			if _, err := agent.RunOrchestrate(context.Background(), "edited goal", "", 5); !errors.Is(err, ErrSpendRail) {
				t.Fatalf("normal admission bypassed budget: %v", err)
			}
			if calls.Load() != 1 {
				t.Fatal("refused retry called planner")
			}
			spend(agent, 0)
			if err := retry(); err != nil {
				t.Fatal(err)
			}
			fresh := waitForRun(t, agent, "2")
			if fresh.Goal != "audit the repository" || fresh.Answer != "audit" || len(fresh.Nodes) != 1 || fresh.Nodes[0].ID != "fresh" || len(fresh.Notes) != 0 {
				t.Fatalf("retry reused old state: %+v", fresh)
			}
			var completed TaskNotice
			waitFor(t, "fresh run landing", func() bool {
				for _, r := range agent.appRunRows() {
					if r.Run == "2" && r.Parent == 0 {
						completed = r
					}
				}
				return completed.State == TaskDone
			})
			before := calls.Load()
			if completed.Retryable || agent.RetryTask(completed.ID) == nil || calls.Load() != before {
				t.Fatal("non-empty ending supported an empty-plan retry")
			}
			old, _ := agent.OrchestrateSnapshot(id)
			if len(old.Notes) != 1 || old.Notes[0] != "Need the repository path." {
				t.Fatalf("retry mutated old run: %+v", old)
			}
			edited, err := agent.RunOrchestrate(context.Background(), "audit /tmp/example", "", 5)
			if err != nil {
				t.Fatal(err)
			}
			if got := waitForRun(t, agent, edited).Goal; got != "audit /tmp/example" {
				t.Fatal(got)
			}
		})
	}
}

// STEERING REACHES THE PLANNER, which is the whole reason a run has a lane
// back into it.
func TestSteeringReachesThePlanner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var (
		seen    = make(chan string, 8)
		holding = make(chan struct{})
	)
	completer := replier(func(messages []ai.Message) string {
		asked := lastUserText(messages)
		switch {
		case isPlannerCall(messages):
			select {
			case seen <- asked:
			default:
			}
			if strings.Contains(asked, "nothing yet") {
				return `{"add":[{"id":"n1","goal":"look"}]}`
			}
			return `{"done":{"brief":"wrap up"}}`
		case strings.Contains(asked, "Ground every claim"):
			return "the write-up"
		default:
			<-holding
			return "looked"
		}
	})
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.AskConsent = true })
	id, err := agent.RunOrchestrate(context.Background(), "look at the thing", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	// The opening call has already happened; the node is running, which is when
	// a person actually types at a run.
	<-seen
	if err := agent.SteerOrchestrate(id, "check the changelog too"); err != nil {
		t.Fatal(err)
	}
	close(holding)

	deadline := time.After(10 * time.Second)
	for {
		select {
		case view := <-seen:
			if strings.Contains(view, "check the changelog too") {
				if !strings.Contains(view, "outranks your plan") {
					t.Fatalf("steering arrived without saying what it outranks")
				}
				waitForRun(t, agent, id)
				return
			}
		case <-deadline:
			t.Fatalf("steering never reached the planner")
		}
	}
}

// ── the models a run runs on ────────────────────────────────────────────────

// runModels answers like [replier] and remembers WHICH MODEL each kind of call
// rode. A role resolution is invisible from outside the run except here: it is
// one option on one request.
type runModels struct {
	mu     sync.Mutex
	answer func(messages []ai.Message) string
	// seen holds the FIRST model each kind was asked on. A run makes several
	// calls of each kind and they all ride the same resolution; the first is the
	// one that cannot have been affected by anything the test did afterwards.
	seen   map[string]string
	effort map[string]provider.Effort
}

func (r *runModels) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	kind := "node"
	switch {
	case isPlannerCall(messages):
		kind = "planner"
	case isTitleCall(messages):
		// AND NEITHER IS THE SESSION'S OWN NAMER. It is started when the first
		// message is accepted rather than when the turn ends (title.go), so it
		// is in flight beside the run's own calls instead of after them —
		// counting it as a node made the first assertion here about the title
		// role's tier.
		kind = "titler"
	case isNameCall(messages):
		// THE NAMER IS NOT PART OF THE RUN. It is one cheap call that turns the
		// run's goal into the two or three words its row is drawn under
		// (taskname.go), on its own role and its own class, and counting it as a
		// node would make every assertion here about the wrong model.
		kind = "namer"
	}
	r.mu.Lock()
	if r.seen == nil {
		r.seen = map[string]string{}
		r.effort = map[string]provider.Effort{}
	}
	if _, told := r.seen[kind]; !told {
		r.seen[kind] = request.Model
		r.effort[kind] = provider.ReasoningEffortFrom(ctx)
	}
	answer := r.answer
	r.mu.Unlock()
	return textResponse(answer(messages)), nil
}

func (r *runModels) reasoning(kind string) provider.Effort {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.effort[kind]
}

func (r *runModels) model(kind string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen[kind]
}

// oneNodeRun is a script for the smallest whole run there is: one node, then
// the write-up.
func oneNodeRun(messages []ai.Message) string {
	asked := lastUserText(messages)
	switch {
	case isPlannerCall(messages):
		if strings.Contains(asked, "nothing yet") {
			return `{"add":[{"id":"n1","goal":"look at the thing"}]}`
		}
		return `{"done":{"brief":"say what was found"}}`
	case strings.Contains(asked, "Ground every claim"):
		return "the write-up (n1)"
	default:
		return "n1 looked"
	}
}

// tierSettings is [roles.Source] as a map literal — the whole seam is a key
// lookup, so a test needs no profile directory (internal/roles says it first).
func tierSettings(pairs map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := pairs[key]
		return value, ok
	}
}

// A RUN IS TWO PURCHASES AND IT MAKES THEM SEPARATELY: with nothing named on
// the turn, the planner thinks on RolePlanner's model and a node runs on
// RoleWorker's — one careful call deciding what happens, cheap ones doing it.
func TestARunResolvesThePlannerAndTheWorkerRoles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	watch := &runModels{answer: oneNodeRun}
	agent, _ := newTestAgent(t, watch, func(config *Config) {
		config.AskConsent = true
		config.RolesSource = tierSettings(map[string]string{
			roles.TierKey(roles.TierMastermind): "test/brain-model",
			roles.TierKey(roles.TierHigh):       "test/careful-model",
			roles.TierKey(roles.TierWorker):     "test/worker-model",
			roles.TierKey(roles.TierLow):        "test/cheap-model",
		})
	})

	id, err := agent.RunOrchestrate(context.Background(), "look at the thing", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, agent, id)

	if got := watch.model("planner"); got != "test/brain-model" {
		t.Fatalf("the planner thought with %q, want the mastermind tier's model", got)
	}
	// A node is WORK, and rides the worker tier — the same seat a task handed
	// off in conversation rides — rather than the small-work tier beside it.
	if got := watch.model("node"); got != "test/worker-model" {
		t.Fatalf("a node ran on %q, want the worker tier's model", got)
	}
}

func TestARunCarriesTheTierEffortToPlannerRequests(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	watch := &runModels{answer: oneNodeRun}
	agent, _ := newTestAgent(t, watch, func(config *Config) {
		config.AskConsent = true
		config.RolesSource = tierSettings(map[string]string{
			roles.TierKey(roles.TierMastermind): "test/brain-model:low",
			roles.TierKey(roles.TierLow):        "test/cheap-model",
		})
	})
	id, err := agent.RunOrchestrate(context.Background(), "look at the thing", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, agent, id)
	if got := watch.model("planner"); got != "test/brain-model" {
		t.Fatalf("planner model = %q, want suffix-free model", got)
	}
	if got := watch.reasoning("planner"); got != provider.EffortLow {
		t.Fatalf("planner effort = %q, want low", got)
	}
}

func TestARunLeavesEffortAbsentWithoutASuffixAndForNamedModels(t *testing.T) {
	for _, tc := range []struct{ name, named string }{
		{name: "plain tier"},
		{name: "named model", named: "test/named-model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			watch := &runModels{answer: oneNodeRun}
			agent, _ := newTestAgent(t, watch, func(config *Config) {
				config.AskConsent = true
				config.RolesSource = tierSettings(map[string]string{
					roles.TierKey(roles.TierMastermind): "test/brain-model:low",
					roles.TierKey(roles.TierLow):        "test/cheap-model",
				})
			})
			if tc.named == "" {
				agent.config.RolesSource = tierSettings(map[string]string{roles.TierKey(roles.TierMastermind): "test/brain-model", roles.TierKey(roles.TierLow): "test/cheap-model"})
			}
			id, err := agent.RunOrchestrate(context.Background(), "look at the thing", tc.named, 5)
			if err != nil {
				t.Fatal(err)
			}
			waitForRun(t, agent, id)
			if got := watch.reasoning("planner"); got != provider.EffortNone {
				t.Fatalf("planner effort = %q, want no context stamp", got)
			}
		})
	}
}

// A PIN OUTRANKS THE TIER, on the run's calls exactly as on every other
// auxiliary call: the ladder is internal/roles' and this file adds no rung.
func TestARunTakesAPlannerPinOverItsTier(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	watch := &runModels{answer: oneNodeRun}
	agent, _ := newTestAgent(t, watch, func(config *Config) {
		config.AskConsent = true
		config.RolesSource = tierSettings(map[string]string{
			roles.PinKey(roles.RolePlanner): "test/pinned-model",
			roles.TierKey(roles.TierHigh):   "test/careful-model",
			roles.TierKey(roles.TierWorker): "test/worker-model",
			roles.TierKey(roles.TierLow):    "test/cheap-model",
		})
	})

	id, err := agent.RunOrchestrate(context.Background(), "look at the thing", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, agent, id)

	if got := watch.model("planner"); got != "test/pinned-model" {
		t.Fatalf("the planner thought with %q, want the pin", got)
	}
	if got := watch.model("node"); got != "test/worker-model" {
		t.Fatalf("a pinned planner moved the workers too: %q", got)
	}
}

// THE TURN'S OWN WORD OUTRANKS EVERYTHING. "orchestrate the migration with
// opus" is a person choosing the model for the work they are commissioning, and
// it takes the whole run — the planner and the nodes both.
func TestARunOnANamedModelIgnoresTheRoles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	watch := &runModels{answer: oneNodeRun}
	agent, _ := newTestAgent(t, watch, func(config *Config) {
		config.AskConsent = true
		config.RolesSource = tierSettings(map[string]string{
			roles.TierKey(roles.TierHigh): "test/careful-model",
			roles.TierKey(roles.TierLow):  "test/cheap-model",
		})
	})

	id, err := agent.RunOrchestrate(context.Background(), "look at the thing", "test/named-model", 5)
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, agent, id)

	for _, kind := range []string{"planner", "node"} {
		if got := watch.model(kind); got != "test/named-model" {
			t.Fatalf("the %s ran on %q, want the model the turn named", kind, got)
		}
	}
}

// AND AN INSTALL THAT CONFIGURED NOTHING RUNS AS IT ALWAYS DID: no tiers, no
// pins, and every call in the run goes to the model the person is talking to,
// which is roles.Resolve's floor and not a failure.
func TestARunWithNoSettingsFallsToTheSessionModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	watch := &runModels{answer: oneNodeRun}
	agent, _ := newTestAgent(t, watch, func(config *Config) { config.AskConsent = true })

	id, err := agent.RunOrchestrate(context.Background(), "look at the thing", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, agent, id)

	for _, kind := range []string{"planner", "node"} {
		if got := watch.model(kind); got != "test/model" {
			t.Fatalf("the %s ran on %q, want the session's own model", kind, got)
		}
	}
}

// ── the write scope ─────────────────────────────────────────────────────────

// A NODE WRITES INSIDE ITS SCOPE AND NOWHERE ELSE, and the refusal is a result
// it can read rather than a turn it loses.
func TestWriteScopeBindsTheHandsThatKnowTheirPath(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.writeScope = []string{"docs", "internal/session/orchestrate.go"}
	})
	guard := writeGuard{agent: agent}

	for _, c := range []struct {
		name    string
		call    ai.ToolCall
		allowed bool
	}{
		{"inside a scoped directory", scopedCall("write", workspace+"/docs/notes.md"), true},
		{"the scoped file itself", scopedCall("edit", workspace+"/internal/session/orchestrate.go"), true},
		{"a sibling the scope does not name", scopedCall("edit", workspace+"/internal/session/loop.go"), false},
		{"above the workspace", scopedCall("write", "/etc/hosts"), false},
		{"a hand with no path", scopedCall("bash", ""), true},
	} {
		_, result, ok := guard.PreAction(context.Background(), nil, nil, c.call)
		if ok != c.allowed {
			t.Errorf("%s: allowed=%v, want %v", c.name, ok, c.allowed)
		}
		if !ok && (!result.isError || !strings.Contains(result.text, "write scope")) {
			t.Errorf("%s: the refusal reads %q", c.name, result.text)
		}
	}

	// And an agent with no scope is exactly the agent it was before the citizen
	// existed.
	plain, plainSpace := newTestAgent(t, &scriptedCompleter{}, nil)
	if _, _, ok := (writeGuard{agent: plain}).PreAction(context.Background(), nil, nil,
		scopedCall("write", plainSpace+"/anywhere.txt")); !ok {
		t.Fatalf("an unscoped agent was refused")
	}
}

// ── the small parts ─────────────────────────────────────────────────────────

func TestWorkerWriteScopeAcceptsSegmentGlobs(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.writeScope = []string{"crew-0-*.md"}
	})
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"crew-0-a.md", true},
		{"crew-1-a.md", false},
		{"../x", false},
		{"crew-0-sub/a.md", false},
	} {
		_, result, allowed := (writeGuard{agent: agent}).PreAction(context.Background(), nil, nil,
			scopedCall("write", workspace+"/"+tc.path))
		if allowed != tc.want {
			t.Errorf("write %s: allowed=%v, want %v; result: %s", tc.path, allowed, tc.want, result.text)
		}
	}
}

func TestWorkerGlobScopeRefusesSymlinkEscapes(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.writeScope = []string{"crew-*"}
	})
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workspace, "crew-outside")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, "crew-inside"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "missing.md"), filepath.Join(workspace, "crew-dangling.md")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"crew-outside/new/deep.md", false},
		{"crew-dangling.md", false},
		{"crew-inside/new/deep.md", true},
	} {
		_, result, allowed := (writeGuard{agent: agent}).PreAction(context.Background(), nil, nil,
			scopedCall("write", filepath.Join(workspace, tc.path)))
		if allowed != tc.want {
			t.Errorf("write %s: allowed=%v, want %v; result: %s", tc.path, allowed, tc.want, result.text)
		}
	}
}

func TestOrchestrateBriefCarriesTheDigestsAndTheBound(t *testing.T) {
	brief := orchestrateBrief("",
		orchestrate.Node{ID: "n3", Goal: "write the migration note", WriteScope: []string{"docs"}},
		[]orchestrate.NodeStatus{{Node: orchestrate.Node{ID: "n1"}, Digest: "the old client is used in four files"}},
		true)
	for _, want := range []string{"write the migration note", "[n1]", "the old client is used in four files", "docs"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("the brief is missing %q:\n%s", want, brief)
		}
	}
	read := orchestrateBrief("", orchestrate.Node{ID: "n1", Goal: "find out"}, nil, true)
	if !strings.Contains(read, "READ-ONLY") {
		t.Fatalf("a node with no scope is told it writes nothing:\n%s", read)
	}
}

// EVERY NODE OF A RUN READS THE PERSON'S OWN WORDS, then the run's goal, then
// its own part. A planner writes each node's goal out of its own reading of the
// run, so a node that could see only that reading has no way to notice a
// requirement the reading dropped — which is exactly what happened when a run
// fanned out eight children that all did the same paraphrased job.
func TestEveryRunNodeIsToldWhatThePersonAskedForAndWhatTheRunIsFor(t *testing.T) {
	worker := &orchestrateExec{
		request: "audit every package for the old pricing constant and write it up in docs/pricing.md",
		goal:    "sweep the repo for hard-coded prices and produce one page",
	}
	brief := orchestrateBrief(worker.root(),
		orchestrate.Node{ID: "n2", Goal: "check internal/billing", WriteScope: []string{"docs"}}, nil, true)

	for _, want := range []string{
		briefAskHeading,
		"audit every package for the old pricing constant and write it up in docs/pricing.md",
		briefWorkHeading,
		"sweep the repo for hard-coded prices and produce one page",
		"YOUR PART OF IT",
		"check internal/billing",
	} {
		if !strings.Contains(brief, want) {
			t.Fatalf("a run node's brief is missing %q:\n%s", want, brief)
		}
	}
	// THE ORDER IS THE CONTRACT: their words, then the run, then this node's
	// piece. A node that met its own goal first would read the rest as footnotes.
	ask := strings.Index(brief, briefAskHeading)
	run := strings.Index(brief, "sweep the repo for hard-coded prices")
	part := strings.Index(brief, "check internal/billing")
	if !(ask < run && run < part) {
		t.Fatalf("the sections are out of order (ask %d, run %d, part %d):\n%s", ask, run, part, brief)
	}
}

// A RUN NOBODY TYPED — one a test scripted, one resumed — has no request, and
// gets no heading over nothing. The run's goal still reaches its nodes.
func TestARunWithNoPersonBehindItGetsNoEmptyHeading(t *testing.T) {
	worker := &orchestrateExec{goal: "sweep the repo for hard-coded prices"}
	brief := orchestrateBrief(worker.root(), orchestrate.Node{ID: "n1", Goal: "check billing"}, nil, true)
	if strings.Contains(brief, briefAskHeading) {
		t.Fatalf("a run nobody asked for quotes somebody:\n%s", brief)
	}
	if !strings.Contains(brief, "sweep the repo for hard-coded prices") {
		t.Fatalf("the run's goal never reached its node:\n%s", brief)
	}
}

// THE PLANNER READS THEIR WORDS ABOVE THE GOAL, because the planner is where a
// paraphrase becomes a set of node goals.
func TestThePlannerSeesThePersonsOwnWordsAboveTheGoal(t *testing.T) {
	view := renderOrchestrateView(orchestrate.View{Goal: "sweep the repo for hard-coded prices"},
		"audit every package for the old pricing constant")
	ask := strings.Index(view, briefAskHeading)
	goal := strings.Index(view, "THE GOAL:")
	if ask < 0 || goal < 0 || ask > goal {
		t.Fatalf("the request is not above the goal (ask %d, goal %d):\n%s", ask, goal, view)
	}
	if !strings.Contains(view, "audit every package for the old pricing constant") {
		t.Fatalf("the planner never sees what was asked for:\n%s", view)
	}
	if bare := renderOrchestrateView(orchestrate.View{Goal: "sweep"}, ""); strings.Contains(bare, briefAskHeading) {
		t.Fatalf("a run nobody typed quotes somebody:\n%s", bare)
	}
}

func TestWorktreePathDegradesToTheSharedTree(t *testing.T) {
	if got := (Config{}).WorktreePath("7-n1"); got != "" {
		t.Fatalf("no root is no path, got %q", got)
	}
	if got := (Config{WorktreeRoot: "/tmp/roots"}).WorktreePath("7-N1"); got != "/tmp/roots/7-n1" {
		t.Fatalf("got %q", got)
	}
}

// ── the helpers ─────────────────────────────────────────────────────────────

func isPlannerCall(messages []ai.Message) bool {
	for _, message := range messages {
		if message.Role == "system" && strings.Contains(messageText(message), "You plan an adaptive run") {
			return true
		}
	}
	return false
}

func lastUserText(messages []ai.Message) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "user" {
			return messageText(messages[index])
		}
	}
	return ""
}

func scopedCall(tool, path string) ai.ToolCall {
	args := "{}"
	if path != "" {
		encoded, _ := json.Marshal(struct {
			Path string `json:"path"`
		}{path})
		args = string(encoded)
	}
	return ai.ToolCall{Function: ai.ToolCallFunction{Name: tool, Arguments: args}}
}

func nextRunEvent(t *testing.T, lane <-chan Event) Event {
	t.Helper()
	select {
	case event, open := <-lane:
		if !open {
			t.Fatalf("the run lane closed")
		}
		return event
	case <-time.After(10 * time.Second):
		t.Fatalf("nothing arrived on the run lane")
		return Event{}
	}
}

func waitForRun(t *testing.T, agent *Agent, id string) orchestrate.Snapshot {
	t.Helper()
	deadline := time.After(15 * time.Second)
	var last orchestrate.Snapshot
	for {
		snap, known := agent.OrchestrateSnapshot(id)
		if !known {
			t.Fatalf("the run %q is not registered", id)
		}
		last = snap
		if snap.Done {
			return snap
		}
		select {
		case <-deadline:
			t.Fatalf("the run never finished: %+v", last)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// AND THE RUN CARRIES THE PLANNER'S MODEL ON ITS SNAPSHOT.
//
// The page that draws the gauge is the page that has to say whose judgement is
// spending it: the planner cuts every node the tank pays for, and with a tier
// set it is a model that appears nowhere else in the conversation. It is the one
// fact about a run a surface cannot derive — internal/orchestrate holds a
// Planner interface and never a model — so it is carried.
func TestARunsSnapshotNamesThePlannersModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	watch := &runModels{answer: oneNodeRun}
	agent, _ := newTestAgent(t, watch, func(config *Config) {
		config.AskConsent = true
		config.RolesSource = tierSettings(map[string]string{
			roles.TierKey(roles.TierMastermind): "test/brain-model",
			roles.TierKey(roles.TierHigh):       "test/careful-model",
			roles.TierKey(roles.TierWorker):     "test/worker-model",
			roles.TierKey(roles.TierLow):        "test/cheap-model",
		})
	})

	id, err := agent.RunOrchestrate(context.Background(), "look at the thing", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	// IT IS TRUE BEFORE THE OPENING PLAN COMES BACK, which is the point of
	// seeding it: the page opens the moment the run starts, and a header that
	// named the planner only after the first node landed would be blank for the
	// whole minute somebody is watching to see what they bought.
	snap, known := agent.OrchestrateSnapshot(id)
	if !known || snap.Planner != "test/brain-model" {
		t.Fatalf("a run that has not planned yet says %q", snap.Planner)
	}
	waitForRun(t, agent, id)
	if snap, _ := agent.OrchestrateSnapshot(id); snap.Planner != "test/brain-model" {
		t.Fatalf("a finished run says %q, want the mastermind tier's model", snap.Planner)
	}
}
