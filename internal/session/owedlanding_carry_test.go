package session

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// ── qloop run23, vector-db t1: the landing turn that went quiet ─────────────
//
// The replay. The manager handed the charts and the research to tasks and said
// it would write the brief once both landed. Both landed in one run whose root
// was owed the person's question, so the report woke the conversation as an
// owed landing: the cheap tier, the answer-from-result page and a six-call
// ceiling. The model began the promised write-up by reading the two sheets.
// The sixth call tripped the ceiling, and the turn sealed with no reply. These
// tests run that sequence against a scripted model and hold the new ending:
// the cheap turn ends and one ordinary turn finishes the work and answers.

// owedLandingRun opens a run store whose root carries the person's question,
// which is what makes its landing owed ([landingOwesAnswer]).
func owedLandingRun(t *testing.T, question string) *beltRun {
	t.Helper()
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "the run", "1",
		"Comparison charts for the vector DB brief", question)
	if err != nil {
		t.Fatalf("open owed run store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if _, err := store.Revise(store.RootID(), plandb.TaskPatch{Question: &question}); err != nil {
		t.Fatalf("mark root answer owed: %v", err)
	}
	return &beltRun{store: store, root: store.RootID()}
}

// landingRequestHolds reports whether any message of one request holds text.
func landingRequestHolds(request []ai.Message, text string) bool {
	for _, message := range request {
		if strings.Contains(messageText(message), text) {
			return true
		}
	}
	return false
}

func TestAnOwedLandingThatOutgrowsItsBoundCarriesTheWorkOnInAnOrdinaryTurn(t *testing.T) {
	question := "deep research brief on vector dbs w/ a comparison table and charts"
	summary := RunSummary{Outcome: beltRunOutcomeDone, Result: "Both charts and their data sheet are done, and the market-facts sheet has all six rows with sources; nothing is running."}

	// Six reads, which is where run23's landing turn stopped. Then the
	// continuation writes the brief and answers.
	steps := make([]step, 0, settleCallCeiling+2)
	for round := 0; round < settleCallCeiling; round++ {
		steps = append(steps, toolStep("ls", fmt.Sprintf(`{"path":"./sheet-%d"}`, round)))
	}
	steps = append(steps, toolStep("ls", `{"path":"./brief"}`), finalText("The brief is written into the Vector DB brief doc."))
	completer := &scriptedCompleter{steps: steps}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = false
		config.RolesSource = tierSettings(map[string]string{
			roles.TierKey(roles.TierLow): "test/cheap-model",
		})
	})
	conversationModel := agent.Model()

	wakes, stopWakes := agent.WatchWakes()
	defer stopWakes()
	agent.deliverBeltRunLanding(owedLandingRun(t, question), summary, RunLanding{})
	beltRunWaitFor(t, "the carried-on turn to answer", func() bool { return completer.requests() == len(steps) })
	waitFor(t, "the carried-on turn to end", func() bool { return !agent.TurnRunning() })

	for index := 0; index < settleCallCeiling; index++ {
		request := completer.request(index)
		if !landingRequestHolds(request, strings.TrimSpace(landingAnswerPrompt)) {
			t.Fatalf("request %d of the reply turn lost its answer-from-result page", index)
		}
		if got := completer.model(index); got != "test/cheap-model" {
			t.Fatalf("request %d of the reply turn rode %q, want the cheap tier", index, got)
		}
	}
	carried := completer.request(settleCallCeiling)
	if landingRequestHolds(carried, strings.TrimSpace(landingAnswerPrompt)) {
		t.Fatal("the carried-on turn still read the short-reply page; it must run as an ordinary turn")
	}
	if got := completer.model(settleCallCeiling); got != conversationModel {
		t.Fatalf("the carried-on turn rode %q, want the conversation's own model %q", got, conversationModel)
	}
	if got := messageText(carried[len(carried)-1]); !isOwedLandingCarryOnLead(got) {
		t.Fatalf("the carried-on turn opened on %q, want the carry-on note", got)
	}
	if !strings.Contains(messageText(carried[len(carried)-1]), fmt.Sprintf("%d calls", settleCallCeiling)) {
		t.Fatalf("the carry-on note does not name the bound it met: %q", messageText(carried[len(carried)-1]))
	}
	// THE STEPS THE CHEAP TURN TOOK ARE NOT TAKEN AGAIN: the continuation reads
	// every read it made, and its first call is the next piece of work.
	for round := 0; round < settleCallCeiling; round++ {
		if !landingRequestHolds(carried, fmt.Sprintf("sheet-%d", round)) {
			t.Fatalf("the carried-on turn lost read %d of the reply turn", round)
		}
	}

	messages := agent.snapshot()
	last := messages[len(messages)-1]
	if last.Role != "assistant" || messageText(last) != "The brief is written into the Vector DB brief doc." {
		t.Fatalf("the conversation ended on %s %q, want the carried-on turn's answer", last.Role, messageText(last))
	}
	opened := 0
	for len(wakes) > 0 {
		<-wakes
		opened++
	}
	if opened != 2 {
		t.Fatalf("the landing opened %d turns, want the reply turn and one carried-on turn", opened)
	}
}

// A REPLY THAT FITS ITS BOUND IS NOT CARRIED ON. The bound trips only on a turn
// that is still working, so an owed landing answered in a few calls costs what
// it did before and opens no second turn.
func TestAnOwedLandingAnsweredWithinItsBoundOpensNoSecondTurn(t *testing.T) {
	question := "What did the repair find?"
	summary := RunSummary{Outcome: beltRunOutcomeDone, Result: "The parser now preserves quoted commas."}
	completer := &scriptedCompleter{steps: []step{
		toolStep("ls", `{"path":"./parser"}`),
		finalText("The repair preserved quoted commas."),
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = false
	})
	wakes, stopWakes := agent.WatchWakes()
	defer stopWakes()
	agent.deliverBeltRunLanding(owedLandingRun(t, question), summary, RunLanding{})
	beltRunWaitFor(t, "the owed landing reply", func() bool { return completer.requests() == 2 })
	waitFor(t, "the reply turn to end", func() bool { return !agent.TurnRunning() })

	for _, message := range agent.snapshot() {
		if isOwedLandingCarryOnLead(messageText(message)) {
			t.Fatal("a reply that answered within its bound was carried on")
		}
	}
	opened := 0
	for len(wakes) > 0 {
		<-wakes
		opened++
	}
	if opened != 1 {
		t.Fatalf("the landing opened %d turns, want one", opened)
	}
}

// THE CARRY-ON LINES ARE THE HARNESS'S OWN, hidden from every row a person
// reads like the other continuation notes.
func TestTheOwedLandingCarryOnIsAHiddenContinuation(t *testing.T) {
	lead := owedLandingCarryOnLead("6 calls")
	if !isContinuationContext(lead) {
		t.Fatal("the carry-on note would be drawn as a row the person reads")
	}
	if !IsCarryOnNotice(owedLandingCarryOnNote) {
		t.Fatal("the carry-on notice would stay on the screen after the turn")
	}
}
