package session

// A hand the harness took away, and the four things that must be true about it.
//
// EVERY TEST HERE IS ONE RUN. SWE-Marathon s4, task 1, 00:37Z–00:40Z: a worker
// building a Java language server in Rust was landed by the no-progress ratchet,
// had `bash`, `read` and `grep` taken off its belt by the landing pass, and was
// answered "Unknown tool: bash" — eighteen bytes — eight times. It retried,
// because eighteen bytes gave it no reason not to; the stuck watch injected
// three [stuck] notes blaming it for the retries; and it then edited two Rust
// source files with no way left to compile them. The task landed reporting work
// that had never been built, and the conversation found the breakage five
// minutes later. withdrawn.go carries the whole account.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// withdrawnCall is one tool call as the model would send it.
func withdrawnCall(id, name, arguments string) ai.ToolCall {
	return ai.ToolCall{ID: id, Function: ai.ToolCallFunction{Name: name, Arguments: arguments}}
}

// ── (i) and (ii): taken, or never held ──────────────────────────────────────

// A HAND THAT WAS TAKEN IS REPORTED AS TAKEN, and a name nobody ever had still
// gets the old answer. The two failures look identical to the belt — one lookup
// that missed — and telling them with the same words is what put a worker in a
// retry loop it could not see the bottom of.
func TestAWithdrawnToolAnswersWithTheWithdrawalAndItsSurvivingSet(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	for _, name := range []string{"bash", "read", "write", "edit"} {
		if !agent.hasTool(name) {
			t.Fatalf("the test agent has no %s to withdraw", name)
		}
	}

	restore := agent.withdrawTools(landingBelt, landingWithdrawal)
	defer restore()

	result := agent.executeTool(context.Background(), nil, nil, withdrawnCall("c1", "bash", `{"command":"cargo build"}`))
	if !result.isError {
		t.Fatal("a call to a withdrawn hand came back as a success")
	}
	if strings.Contains(result.text, "Unknown tool") {
		t.Fatalf("a hand the harness took was called unknown: %q", result.text)
	}
	// The three facts the eighteen-byte answer was missing: why it is gone, what
	// is left BY NAME, and what to do with what is left.
	if !strings.Contains(result.text, landingWithdrawal) {
		t.Fatalf("the withdrawal does not say why the hand is gone: %q", result.text)
	}
	for _, surviving := range []string{"edit", "write"} {
		if !strings.Contains(result.text, surviving) {
			t.Fatalf("the withdrawal does not name %s, which the model still has: %q", surviving, result.text)
		}
	}
	if strings.Contains(result.text, "grep") || strings.Contains(result.text, "cargo") {
		t.Fatalf("the withdrawal offers a hand that is gone: %q", result.text)
	}
	if !strings.Contains(result.text, "again cannot bring it back") {
		t.Fatalf("the withdrawal does not tell the model to stop retrying: %q", result.text)
	}
	// And it is the HARNESS'S failure, which is what keeps it out of every
	// counter that judges the model by its steps.
	if !result.harness {
		t.Fatal("a withdrawn hand's refusal was booked as the model's own failure")
	}

	// A NAME NOBODY EVER HAD IS STILL UNKNOWN. That one really is a sentence
	// about the model, and it must keep its own words.
	unknown := agent.executeTool(context.Background(), nil, nil, withdrawnCall("c2", "transmogrify", "{}"))
	if unknown.text != "Unknown tool: transmogrify" {
		t.Fatalf("a tool nobody ever had answers %q, want the unknown-tool sentence", unknown.text)
	}
	if unknown.harness {
		t.Fatal("a name nobody ever had was booked as a harness failure")
	}

	// AND THE BELT COMES BACK WHOLE. A restored belt has no withdrawal standing
	// over it, so `bash` is an ordinary hand again.
	restore()
	if _, withdrawn := agent.withdrawalNotice("bash"); withdrawn {
		t.Fatal("a restored belt still reports its hands as withdrawn")
	}
}

// A DOOR THAT REFUSED THE CALL IS THE HARNESS'S ANSWER TOO. The tool never ran
// and the world never saw the call; what came back was written on this side of
// the wall by a policy. It is marked at the one chokepoint every veto passes
// through, so a pre-action citizen added later cannot forget to say so.
func TestARefusedDoorIsMarkedAsTheHarnesssOwnFailure(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ApprovalPolicy = &approval.Policy{
			Default: approval.ActionAllow,
			Tools:   map[string]approval.Action{"write": approval.ActionDeny},
		}
	})
	episode := agent.newEpisode()

	refused := agent.executeTool(context.Background(), episode, nil,
		withdrawnCall("c1", "write", `{"path":"a.md","content":"x"}`))
	if !refused.isError || !strings.HasPrefix(refused.text, "denied by approval rule:") {
		t.Fatalf("the denied call answered %+v, want the gate's own refusal", refused)
	}
	if !refused.harness {
		t.Fatal("a door that refused the call booked its refusal against the model")
	}

	// AND AN ORDINARY FAILURE IS STILL THE MODEL'S. A read of a path that is not
	// there is the world answering, and a counter must go on counting it.
	missing := agent.executeTool(context.Background(), episode, nil,
		withdrawnCall("c2", "read", fmt.Sprintf(`{"path":%q}`, workspace+"/nowhere.txt")))
	if !missing.isError {
		t.Fatal("reading a file that is not there came back as a success")
	}
	if missing.harness {
		t.Fatalf("a failure the WORLD produced was booked as the harness's: %q", missing.text)
	}
}

// ── (iii): the harness's steps are not the model's ──────────────────────────

// THE RATCHET DOES NOT COUNT THE HARNESS'S OWN REFUSALS. Five calls to a hand
// that was taken away, against a threshold of two: every one of them saves
// nothing, moves no worktree and teaches nothing new, so the old counter read
// them as five steps of spinning and killed the node on the third — for
// retrying a call the harness had disarmed.
//
// The stuck watch is the same law one layer up: it must not name a repetition
// the harness manufactured.
func TestHarnessMadeFailuresNeitherAdvanceNorResetTheNoProgressCount(t *testing.T) {
	var script []step
	for index := 0; index < 5; index++ {
		id := fmt.Sprintf("gone-%d", index)
		script = append(script, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse(id, "bash", `{"command":"cargo build --release"}`), nil
		})
	}
	script = append(script, finalText("no bash, so here is what I have"))

	completer := &scriptedCompleter{steps: script}
	nest := newNest(t, completer, nil)
	// The belt is narrowed BEFORE the run, which is the state the measured worker
	// was in when it made these calls.
	defer nest.node.withdrawTools(landingBelt, landingWithdrawal)()

	done, stopped := runParent(t, nest, taskLimits{maxSteps: 200, noProgress: 2})
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the node never finished")
	}
	if *stopped != "" {
		t.Fatalf("the node was stopped as %q — the harness counted its own refusals against the model", *stopped)
	}

	// AND NOBODY WAS SCOLDED FOR THE RETRIES. Three [stuck] notes were injected
	// in the real run, each of them telling the model it had repeated a call the
	// harness itself had answered.
	nest.node.mu.Lock()
	messages := append([]ai.Message(nil), nest.node.messages...)
	nest.node.mu.Unlock()
	for _, message := range messages {
		if strings.Contains(messageText(message), "[stuck]") {
			t.Fatalf("the model was nudged for repeating a call the harness refused: %q", messageText(message))
		}
	}
}

// ── (iv): what it saved after the withdrawal was never checked ──────────────

// landingWorker is a worker that RUNS ITS WORK: it builds, over and over, with
// the same answer every time — the spin the ratchet exists to catch — and then
// does whatever it was built to do when it is told to land.
//
// It answers off the last message rather than off a step count because the
// ratchet fires ASYNCHRONOUSLY: the runner counts finished calls on its own side
// of the wall, so which request is the landing one is not knowable in advance,
// and a fixed script lands whichever step it happens to be up to.
type landingWorker struct{ saves bool }

func (w *landingWorker) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	last := ""
	if len(messages) > 0 {
		last = messageText(messages[len(messages)-1])
	}
	switch {
	case strings.Contains(last, "LAND NOW"):
		if w.saves {
			return toolResponse("save", "write", `{"path":"parser.rs","content":"fn parse() {}"}`), nil
		}
		return textResponse("here is what I found; nothing left to write"), nil
	case strings.Contains(last, "Successfully wrote"):
		return textResponse("I fixed the ranges; I could not build them"), nil
	default:
		return toolResponse("build", "bash", `{"command":"echo the same answer every time"}`), nil
	}
}

// EDITS MADE AFTER THE HANDS CAME OFF ARE UNVERIFIED, AND THE LANDING SAYS SO.
// The measured node had been building all run, lost `bash` with the narrowing,
// and then wrote two source files as its last act. Nothing compiled them and its
// report did not say so, so the parent read finished work.
func TestEditsSavedAfterTheWithdrawalAreReportedUnverified(t *testing.T) {
	nest := newNest(t, &landingWorker{saves: true}, nil)
	done, stopped := runParent(t, nest, taskLimits{maxSteps: 200, noProgress: 2})
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the node never finished")
	}

	if !strings.Contains(*stopped, "without progress") {
		t.Fatalf("the node landed %q, want the no-progress threshold — the test's own premise", *stopped)
	}
	if !strings.Contains(*stopped, unverifiedEdits) {
		t.Fatalf("the landing reports %q, want it to say the edits after the withdrawal are unverified", *stopped)
	}
	// IT IS THE VOCABULARY A LANDING ALREADY HAS, not a new state.
	if !strings.Contains(*stopped, incompleteLead) {
		t.Fatalf("the unverified sentence invented its own vocabulary: %q", *stopped)
	}
}

// AND A NODE THAT SAVED NOTHING IS NOT ACCUSED OF IT. The sentence is written
// only when all three facts hold — it was running a check, the check was taken
// away, and it saved something afterwards — so an ordinary stopped node's report
// is exactly what it always was.
func TestALandingThatSavesNothingIsNotCalledUnverified(t *testing.T) {
	nest := newNest(t, &landingWorker{}, nil)
	done, stopped := runParent(t, nest, taskLimits{maxSteps: 200, noProgress: 2})
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the node never finished")
	}
	if !strings.Contains(*stopped, "without progress") {
		t.Fatalf("the node landed %q, want the no-progress threshold — the test's own premise", *stopped)
	}
	if strings.Contains(*stopped, unverifiedEdits) {
		t.Fatalf("a node that saved nothing in its landing was told its edits are unverified: %q", *stopped)
	}
}

// ── the record itself ───────────────────────────────────────────────────────

// A withdrawal knows what went and what stayed, and answers about nothing else.
func TestAWithdrawalRecordsWhatWentAndWhatStayed(t *testing.T) {
	withdrawal := &toolWithdrawal{
		reason: landingWithdrawal,
		gone:   map[string]bool{"bash": true, "read": true},
		kept:   []string{"write", "edit"},
	}
	if _, ok := withdrawal.notice("write"); ok {
		t.Fatal("a hand that is still on the belt was reported as withdrawn")
	}
	if _, ok := withdrawal.notice("transmogrify"); ok {
		t.Fatal("a name nobody ever had was reported as withdrawn")
	}
	if !withdrawal.withdrewChecking() {
		t.Fatal("a withdrawal that took bash does not know it took the checking hand")
	}
	reading := &toolWithdrawal{reason: "x", gone: map[string]bool{"read": true}, kept: []string{"bash"}}
	if reading.withdrewChecking() {
		t.Fatal("losing a reading hand was mistaken for losing the check")
	}
	var absent *toolWithdrawal
	if _, ok := absent.notice("bash"); ok {
		t.Fatal("a whole belt reported a withdrawal")
	}
	if absent.withdrewChecking() {
		t.Fatal("a whole belt reported losing its check")
	}
}

// The landing belt and the withdrawal it produces agree with each other: what
// the notice offers is exactly what the model was left holding.
func TestTheWithdrawalNoticeNamesTheBeltTheModelActuallyHas(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	restore := agent.withdrawTools(landingBelt, landingWithdrawal)
	defer restore()

	notice, withdrawn := agent.withdrawalNotice("read")
	if !withdrawn {
		t.Fatal("read survived a landing belt")
	}
	for _, tool := range agent.beltTools() {
		if !strings.Contains(notice, tool.Name) {
			t.Fatalf("the notice does not offer %s, which is on the belt: %q", tool.Name, notice)
		}
	}
	if !strings.Contains(landingInstruction(agent.beltTools()), "edit") {
		t.Fatal("the landing instruction and the belt disagree")
	}
}
