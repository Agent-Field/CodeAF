package session

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the checker's window, told (#941) ───────────────────────────────────────
//
// Correct work landed `your call` because the checker's own model call was cut
// at its thirty-second share of a one-minute window, twice, while a reasoning
// model thought without writing a word. Nothing on the wire had said how long it
// had, its calls were planned as a working node's, what it had read was thrown
// away with the stream, and the landing read as a verdict on the work. These
// pin the four halves of the fix, on the real road where the road can be driven
// and on the real adapter where the claim is about a request body.

// believedLedger scripts what the lane belief holds for one machine: its median
// output rate, known well. It is the reading the adapter sizes a told wall
// against (internal/provider's effortladder.go), so what the wire test pins is
// the budget derived from the same belief the controller waits on.
type believedLedger struct {
	mu      sync.Mutex
	beliefs map[lanes.ID]lanes.Belief
}

func (l *believedLedger) Note(lanes.Sighting)           {}
func (l *believedLedger) NoteOutcome(lanes.Outcome)     {}
func (l *believedLedger) Prime(lanes.Row, float64)      {}
func (l *believedLedger) Beliefs(string) []lanes.Belief { return nil }
func (l *believedLedger) Belief(id lanes.ID) (lanes.Belief, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	belief, ok := l.beliefs[id]
	return belief, ok
}

func believeCheckerLane(t *testing.T, model, lane string, tokensPerSecond float64) {
	t.Helper()
	id := lanes.ID{Model: model, Lane: lane}
	lanes.Default().SetLedger(&believedLedger{beliefs: map[lanes.ID]lanes.Belief{
		id: {ID: id, Rate: lanes.Posterior{X: math.Log(tokensPerSecond), P: 0.01}},
	}})
	t.Cleanup(func() { lanes.Default().SetLedger(nil) })
}

// thinksUnasked is a reasoning model that thinks whether or not a request asks
// it to, at the top of its ladder — the shape of the checker's model in #941.
func thinksUnasked(string) (provider.ReasoningProfile, bool) {
	return provider.ReasoningProfile{Mandatory: true, Default: "max"}, true
}

// reasoningBudget is the thinking allowance one recorded body carried, and zero
// when it carried none.
func (s *reasoningServer) reasoningBudget(t *testing.T, index int) int {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if index >= len(s.bodies) {
		t.Fatalf("request %d was never made; the server saw %d", index, len(s.bodies))
	}
	raw, ok := s.bodies[index]["reasoning"]
	if !ok {
		return 0
	}
	var knob struct {
		MaxTokens int `json:"max_tokens"`
	}
	if err := json.Unmarshal(raw, &knob); err != nil {
		t.Fatalf("request %d reasoning: %v (%s)", index, err, raw)
	}
	return knob.MaxTokens
}

// THE CHECK'S BUDGET IS ITS OWN WINDOW, AT ITS OWN MACHINE'S PACE.
//
// The window is the product's: a check with nothing to run gets the reading
// deadline, and one call may hold its share of it — the thirty seconds #941 was
// cut at. A call opened under that share, through the one client door, carries a
// thinking allowance the adapter derived from the share and the lane's believed
// rate: more on a fast machine than on a slow one, never the whole of what the
// share could hold (the verdict keeps the rest), and exactly what the share
// itself would imply, less only the moments between opening the window and the
// request leaving. The control is the same call with no window opened, which
// says nothing about thinking at all — as every call this package makes but the
// windowed ones still does.
func TestAWindowedCallIsToldTheTimeItHasLeft(t *testing.T) {
	const model, lane = "deepseek/deepseek-v4-flash", "DeepInfra"
	share := newAuditPace(auditReadingDeadline, time.Now()).call
	if share != 30*time.Second {
		t.Fatalf("the reading window's share is %v; #941's arithmetic is a thirty-second call", share)
	}
	server := newReasoningServer(t, answerOK)
	client, err := provider.NewClient(provider.Config{
		APIKey: "k", BaseURL: server.URL, Model: model, ReasoningProfile: thinksUnasked,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	agent, _ := newTestAgent(t, client, nil)
	ask := []ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "check this"}}}}
	send := func(ctx context.Context) int {
		t.Helper()
		before := server.requests()
		if _, err := agent.completeWithModel(provider.WithLaneChoice(ctx, lanes.Choice{Order: []string{lane}}), ask, model); err != nil {
			t.Fatalf("completeWithModel: %v", err)
		}
		return server.reasoningBudget(t, before)
	}
	budgetAt := func(tokensPerSecond float64) (told, implied int) {
		believeCheckerLane(t, model, lane, tokensPerSecond)
		ctx, done := openCallWindow(context.Background(), share, callWindow{})
		defer done()
		told = send(ctx)
		// What the share itself implies, asked of the same derivation without
		// the door in the way: the ceiling the told figure may not pass.
		implied = send(provider.WithThinkingWall(context.Background(), share))
		return told, implied
	}

	fast, fastImplied := budgetAt(200)
	slow, slowImplied := budgetAt(20)
	if fast <= slow {
		t.Fatalf("a check on a machine believed at 200 tok/s was told %d and one at 20 tok/s %d; the budget is the machine's pace, not a constant", fast, slow)
	}
	for rate, pair := range map[float64][2]int{200: {fast, fastImplied}, 20: {slow, slowImplied}} {
		told, implied := pair[0], pair[1]
		whole := int(rate * share.Seconds())
		if told <= 0 || told >= whole {
			t.Fatalf("at %.0f tok/s the check was told %d of the %d tokens its share holds; the verdict keeps the rest", rate, told, whole)
		}
		// THE DOOR HANDS THE TIME LEFT AND NOTHING ELSE: at most the share, and
		// short of it by no more than one second's writing.
		if told > implied || told < implied-int(rate) {
			t.Fatalf("at %.0f tok/s the told budget is %d and the share implies %d; the door handed a different wall", rate, told, implied)
		}
	}

	// THE CONTROL: no window, no word about thinking.
	believeCheckerLane(t, model, lane, 200)
	if budget := send(context.Background()); budget != 0 {
		t.Fatalf("a call nobody opened a window for carried a thinking budget of %d", budget)
	}
}

// checkerCall is what one of the checker's requests arrived with, read off the
// context the client door handed the completer.
type checkerCall struct {
	role     lanes.Role
	window   callWindow
	opened   bool
	left     time.Duration
	thinking provider.Effort
}

func readCheckerCall(ctx context.Context) checkerCall {
	call := checkerCall{role: provider.RoleFrom(ctx), thinking: provider.ReasoningEffortFrom(ctx)}
	call.window, call.opened = ctx.Value(callWindowKey{}).(callWindow)
	if deadline, bounded := ctx.Deadline(); bounded {
		call.left = time.Until(deadline)
	}
	return call
}

// THE CHECKER IS A JUDGE, UNDER A WINDOW IT IS TOLD — AND A CHECK THAT ANSWERS
// IN TIME IS OTHERWISE UNCHANGED.
//
// On the real road: a node writes a file, the checker answers VERIFIED in one
// call, and the work lands done. What that one call arrived with is the pin. Its
// role is a judge's and not a leaf's — the worker beside it is still a leaf, so
// this is the checker's own property and not a change to every node — its
// window was opened and it is no longer than one call's share, and nothing
// switched its thinking off. No second ask was made.
func TestTheCheckersCallIsAJudgesUnderAToldWindow(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	var mu sync.Mutex
	var worker, checker []checkerCall
	completer := &routedCompleter{
		parent: []step{proposeCall("Add the greeting", "write greet.go"), finalText("handed off")},
		child: []step{
			func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
				mu.Lock()
				worker = append(worker, readCheckerCall(ctx))
				mu.Unlock()
				return writeCall("call-src", "greet.go", "package greet\n\nfunc Greet() string { return \"hi\" }\n")(ctx, messages)
			},
			finalText("Wrote greet.go with the greeting."),
		},
		audit: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			mu.Lock()
			checker = append(checker, readCheckerCall(ctx))
			mu.Unlock()
			return textResponse("VERIFIED — read greet.go, the greeting is there"), nil
		}},
	}
	const window = 4 * time.Second
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.auditWindow = window
	})
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "add a greeting"))
	node := graph.node(1)
	waitDoneNode(t, node)

	if notice := node.notice(); notice.State != TaskDone {
		t.Fatalf("state = %q, want done (report %q)", notice.State, notice.Report)
	}
	if calls := completer.auditCalls(); calls != 1 {
		t.Fatalf("a check that answered in time was asked %d times, want once", calls)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(checker) != 1 || len(worker) == 0 {
		t.Fatalf("saw %d checker and %d worker calls", len(checker), len(worker))
	}
	call := checker[0]
	if call.role != lanes.RoleJudge {
		t.Fatalf("the checker's call was planned as %q; a gate reading an answer is a judge", call.role)
	}
	if role := worker[0].role; role != lanes.RoleLeafAttached && role != lanes.RoleLeafUnattended {
		t.Fatalf("the worker's call was planned as %q; only the checker is a judge", role)
	}
	if !call.opened || call.window.answer {
		t.Fatalf("the checker's call was not sent under a told window (opened %v, answer-only %v)", call.opened, call.window.answer)
	}
	if limit := window / auditCallShare; call.left <= 0 || call.left > limit {
		t.Fatalf("the checker's call had %v left, want no more than one call's share, %v", call.left, limit)
	}
	if call.thinking == provider.EffortOff {
		t.Fatal("a check that was never cut had its thinking switched off")
	}
}

// A CUT CHECK KEEPS WHAT IT READ.
//
// The checker opens the file the work wrote, then thinks until its share of the
// window cuts it — #941's shape, where it had read all twenty files. The next ask
// goes to THE SAME CHECKER over the same transcript: the file it read is in front
// of it, the one sentence it is sent demands the word, and its thinking is
// switched off. It answers, and the correct work lands done instead of `your
// call`. No fresh checker re-read anything.
func TestACutCheckIsAskedForItsWordOverWhatItRead(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	var mu sync.Mutex
	var asked checkerCall
	completer := &routedCompleter{
		parent: []step{proposeCall("Add the greeting", "write greet.go"), finalText("handed off")},
		child: []step{
			writeCall("call-src", "greet.go", "package greet\n\nfunc Greet() string { return \"hi\" }\n"),
			finalText("Wrote greet.go with the greeting."),
		},
		audit: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("read-1", "read", `{"path":"greet.go"}`), nil
			},
			// THINKING PAST ITS SHARE: nothing comes back until the window cuts it.
			func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
			func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				mu.Lock()
				asked = readCheckerCall(ctx)
				mu.Unlock()
				return textResponse("VERIFIED — read greet.go, Greet returns the greeting"), nil
			},
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.auditWindow = 2 * time.Second
	})
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "add a greeting"))
	node := graph.node(1)
	waitDoneNode(t, node)

	notice := node.notice()
	if notice.State != TaskDone {
		t.Fatalf("a cut check over correct work landed %q, want done:\n%s", notice.State, notice.Report)
	}
	if calls := completer.auditCalls(); calls != 3 {
		t.Fatalf("the checker was asked %d times, want 3: the read, the call that was cut, and the word", calls)
	}
	third := completer.auditAskedAt(2)
	if last := messageText(third[len(third)-1]); !strings.Contains(last, auditNudge) {
		t.Fatalf("the ask after the cut was not the demand for the word:\n%s", last)
	}
	// AND WHAT IT READ WAS IN FRONT OF IT: the file's contents rode the same
	// transcript, which a fresh checker's evidence packet would not carry.
	carried := false
	for _, message := range third {
		if strings.EqualFold(message.Role, "tool") && strings.Contains(messageText(message), "func Greet()") {
			carried = true
		}
	}
	if !carried {
		t.Fatal("the ask after the cut did not carry the file the checker had already read")
	}
	if first := completer.auditAskedAt(0); len(third) <= len(first) {
		t.Fatalf("the ask after the cut had %d messages and the first %d; a fresh checker started over", len(third), len(first))
	}
	mu.Lock()
	defer mu.Unlock()
	if !asked.opened || !asked.window.answer {
		t.Fatalf("the word was not asked as an answer (opened %v, answer-only %v)", asked.opened, asked.window.answer)
	}
	if asked.thinking != provider.EffortOff {
		t.Fatalf("the word was asked with thinking %q, want it switched off", asked.thinking)
	}
}

// A CHECK CUT WHILE ITS TOOL WAS STILL RUNNING IS ASKED OVER A TRANSCRIPT THE
// PROVIDER WILL TAKE.
//
// The sibling above cuts BETWEEN rounds, which is the easy half: the reply is
// text or nothing, and there is nothing half-written to leave behind. This one
// cuts in the middle of a tool round, which is the case the ask road is actually
// reached from — [Agent.afterTheCut] sends the same checker back whenever it
// holds a tool receipt, and a tool receipt is exactly what a running tool is
// about to become.
//
// THE TURN LOOP IS WHAT MAKES THAT SAFE, AND IT IS ONE LINE: a batch whose tools
// were cut still records a tool message for every call it issued (loop.go, the
// loop under `results := a.runToolsWarm`), so the transcript the second ask
// rides has no assistant tool_call without its answer. The ONE exit that writes
// nothing is `abandoned(ctx)` (loop.go's read of [abandoned]) — a turn the
// session let go of because a person started another one — and a deadline is
// never that. What is asserted here is the property rather than the line: every
// call in the second ask's messages is paired, which is what a provider 400s on
// when it is not true.
func TestACheckCutMidToolIsAskedOverATranscriptThatPairs(t *testing.T) {
	started := make(chan struct{})
	said := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("verify-1", "verify", `{}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("VERIFIED — the check ran and the greeting is there"), nil
		},
	}}
	checker, _ := newTestAgent(t, said, nil)
	checker.tools = []bare.Tool{blockingTool("verify", started)}
	parent, _ := newTestAgent(t, &scriptedCompleter{}, nil)

	// THE CUT LANDS INSIDE THE TOOL because the tool never returns: the scripted
	// reply is instant, the call starts at once, and the window is what ends the
	// round. Nothing here sleeps — the bound IS the clock.
	asked, done := openCallWindow(context.Background(), 300*time.Millisecond, callWindow{})
	events, err := checker.Submit(asked, "check the work against the acceptance")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	for range events {
	}
	<-started
	if asked.Err() == nil {
		t.Fatal("the window did not cut the call; this test is about nothing")
	}
	done()

	// The branch the production road takes: a checker holding a tool receipt is
	// asked for its word rather than replaced ([Agent.afterTheCut]).
	if len(lastToolReceipts(checker, 1)) == 0 {
		t.Fatal("a checker cut in the middle of its tool round kept no receipt, so the ask road is never reached")
	}

	pace := newAuditPace(auditReadingDeadline, parent.now())
	answer := parent.askForTheWord(context.Background(), checker, pace, io.Discard)
	if !answer.answered || !answer.verified {
		t.Fatalf("the ask after a mid-tool cut was not answered (answered %v, verified %v): %s",
			answer.answered, answer.verified, answer.report())
	}
	second := said.request(1)
	if len(second) == 0 {
		t.Fatal("the second ask never left")
	}
	if last := messageText(second[len(second)-1]); !strings.Contains(last, auditNudge) {
		t.Fatalf("the ask after the cut was not the demand for the word:\n%s", last)
	}
	// EVERY CALL IS ANSWERED IN THE MESSAGES THAT WENT OUT. A tool_call with no
	// tool message under it is the 400 this whole road would die of.
	answered := map[string]bool{}
	for _, message := range second {
		if strings.EqualFold(message.Role, "tool") {
			answered[message.ToolCallID] = true
		}
	}
	calls := 0
	for _, message := range second {
		for _, call := range message.ToolCalls {
			calls++
			if !answered[call.ID] {
				t.Fatalf("the ask after a mid-tool cut carries call %q with no result under it; "+
					"the transcript it rides is one a provider refuses", call.ID)
			}
		}
	}
	if calls == 0 {
		t.Fatal("the second ask carries no call at all, so the cut tool round is not in front of the checker")
	}
}

// A CHECK THE CLOCK CUT READS AS THE CLOCK, AND ONLY THAT ONE DOES.
//
// Both calls are cut before the checker has read anything, so there is nothing
// to ask the word over and the window closes: the honest non-answer survives, is
// still the person's call, and now names the checker — the row asks `the check
// ran out of time`, and the report under it never says nobody could check the
// work. The control is a checker that answers neither word on either try, which
// is not the clock and still reads `nobody could check it`.
func TestACheckTheClockCutNamesTheClockNotTheWork(t *testing.T) {
	land := func(t *testing.T, audit []step, window time.Duration) TaskNotice {
		t.Helper()
		repo := newGoModuleRepo(t)
		t.Setenv("HOME", t.TempDir())
		completer := &routedCompleter{
			parent: []step{proposeCall("Add the greeting", "write greet.go"), finalText("handed off")},
			child: []step{
				writeCall("call-src", "greet.go", "package greet\n\nfunc Greet() string { return \"hi\" }\n"),
				finalText("Wrote greet.go with the greeting."),
			},
			audit: audit,
		}
		agent, _ := newTestAgent(t, completer, func(config *Config) {
			config.Workspace = repo
			config.AskConsent = false
			config.TaskAutoApproveSeconds = 0
			config.auditWindow = window
		})
		graph := agent.graph()
		collect(t, mustSubmit(t, agent, "add a greeting"))
		node := graph.node(1)
		waitDoneNode(t, node)
		return node.notice()
	}
	hang := func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	t.Run("cut", func(t *testing.T) {
		notice := land(t, []step{hang, hang}, 40*time.Millisecond)
		if notice.State != TaskUnverified {
			t.Fatalf("state = %q, want the person's call — nothing merges on a non-answer:\n%s", notice.State, notice.Report)
		}
		if reason := taskAskOf(notice.StatusFacts()).Reason; reason != taskAskTimeReason {
			t.Fatalf("the row asks %q, want %q", reason, taskAskTimeReason)
		}
		if !strings.HasPrefix(notice.Report, taskAskTimeReason+yourCallDash) ||
			!strings.Contains(notice.Report, "without answering and was abandoned") {
			t.Fatalf("the report does not lead with the clock and the call it cut:\n%s", notice.Report)
		}
		if strings.Contains(notice.Report, taskAskCheckReason) {
			t.Fatalf("a check the clock cut reads as one nobody could make:\n%s", notice.Report)
		}
	})
	t.Run("control", func(t *testing.T) {
		wandered := verdict("I looked at the change and have some thoughts.")
		notice := land(t, []step{wandered, wandered, wandered, wandered}, 0)
		if reason := taskAskOf(notice.StatusFacts()).Reason; reason != taskAskCheckReason {
			t.Fatalf("a checker that answered neither word reads %q, want %q:\n%s", reason, taskAskCheckReason, notice.Report)
		}
		if strings.Contains(notice.Report, taskAskTimeReason) {
			t.Fatalf("a checker that answered neither word is blamed on the clock:\n%s", notice.Report)
		}
	})
}

// THE SENTENCES, ONE AT A TIME. A non-answer the clock decided leads with the
// clock and the question is read back off that lead, the way every surface reads
// it; a non-answer nobody's clock decided keeps the question it always had.
func TestTheClockLeadIsWrittenOnceAndReadBack(t *testing.T) {
	cut := noVerdict(checkerStalled(30*time.Second), "").ranOutOfTime().andTheWindowClosed()
	report := cut.lookOutcome(TaskFacts{})
	const want = "the check ran out of time — one call ran 30s without answering and was abandoned · the window closed before a second"
	if report != want {
		t.Fatalf("the landing reads\n  %q\nwant\n  %q", report, want)
	}
	if reason := taskAskOf(TaskFacts{State: TaskUnverified, Report: report}).Reason; reason != taskAskTimeReason {
		t.Fatalf("the row read %q back off that landing, want %q", reason, taskAskTimeReason)
	}
	// A sentence that already opens with the clock is the lead, said once.
	if never := noVerdict(checkerRanOut(time.Minute), "").ranOutOfTime().lookOutcome(TaskFacts{}); strings.Count(never, taskAskTimeReason) != 1 {
		t.Fatalf("the clock is said %d times:\n%s", strings.Count(never, taskAskTimeReason), never)
	}
	// Two calls the clock cut open with the clock as well.
	if twice := noVerdict(checkerStalled(29*time.Second), "").ranOutOfTime().twice(); !strings.HasPrefix(twice.evidence[0], taskAskTimeReason+yourCallDash+askedTwice) {
		t.Fatalf("two cut calls read %q", twice.evidence[0])
	}
	// AND THE OTHER COMPOSER OF A NON-ANSWER NEVER WRITES THIS LEAD. An
	// unattended run takes the work as it stands and lands it DONE
	// (task_run.go's [Agent.landUnchecked]), so its prose is never read for the
	// check's question — which is the whole reason reading the lead is safe.
	// A road that landed takenAsItStands' words on a your-call node would ask
	// "nobody could check it" over a clock that ran out, and this is where that
	// is caught.
	taken := takenAsItStands(noVerdict(checkerStalled(30*time.Second), "").ranOutOfTime())
	if strings.HasPrefix(taken, taskAskTimeReason) || strings.HasPrefix(taken, taskAskCheckReason) {
		t.Fatalf("the unattended landing opens with a your-call lead, which only lookOutcome writes:\n%s", taken)
	}
	if !strings.HasPrefix(taken, takenAsItStandsLead) {
		t.Fatalf("the unattended landing opens with %q, want the take's own lead %q", taken, takenAsItStandsLead)
	}

	// THE CONTROL: a checker that could not start is not the clock.
	failed := noVerdict("the checker could not start: no model", "").lookOutcome(TaskFacts{})
	if !strings.HasPrefix(failed, taskAskCheckReason+yourCallDash) {
		t.Fatalf("a checker that could not start reads %q", failed)
	}
	if reason := taskAskOf(TaskFacts{State: TaskUnverified, Report: failed}).Reason; reason != taskAskCheckReason {
		t.Fatalf("the row read %q off it, want %q", reason, taskAskCheckReason)
	}
}
