package session

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/manual"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the harness ─────────────────────────────────────────────────────────────

// runCounter is a belt tool that counts how many times it actually ran and
// answers with a fresh line every time, so that a test about the silent ladder
// is not accidentally also a test of the ledger rule beside it.
func runCounter(name string, ran *atomic.Int64) bare.Tool {
	return bare.Tool{
		Name:        name,
		Description: "counts its own executions",
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			return fmt.Sprintf("%s ran %d", name, ran.Add(1)), false, nil
		},
	}
}

// silentCall is one step of a script: a tool call with no visible text beside
// it, which is the shape the write-your-notes rule is about.
func silentCall(index int) step {
	id, arguments := fmt.Sprintf("look-%d", index), fmt.Sprintf(`{"path":"file-%d"}`, index)
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse(id, "look", arguments), nil
	}
}

// spokenCall is the same step with a note written beside it — the submission
// that meets the rule.
func spokenCall(index int, note string) step {
	id, arguments := fmt.Sprintf("look-%d", index), fmt.Sprintf(`{"path":"file-%d"}`, index)
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponseWithText(id, "look", arguments, note), nil
	}
}

func endingStep() step {
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("done"), nil
	}
}

// enforcedRungAt is how many silent tool-using replies it takes to reach the
// rung where the rule stops being advice. It is derived from the constants
// rather than typed, so a test cannot pin a number the loop does not apply.
func enforcedRungAt() int { return silentThreshold(silentEnforceRung - 1) }

// heldToolResults is every tool result in the transcript that is the rule's
// demand rather than a tool's answer.
func heldToolResults(a *Agent) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var held []string
	for _, message := range a.messages {
		if message.Role != "tool" {
			continue
		}
		if text := messageContentText(message); strings.HasPrefix(text, "[held]") {
			held = append(held, text)
		}
	}
	return held
}

// ── the acceptance the issue states ─────────────────────────────────────────

// A model that never writes notes gets its tools withheld at the escalation
// rung: the submission after the second advisory is answered with the demand and
// none of its calls is run.
func TestAModelThatWillNotWriteNotesHasItsToolsHeldAtTheEnforcedRung(t *testing.T) {
	rung := enforcedRungAt()
	steps := make([]step, 0, rung+2)
	for index := 0; index <= rung; index++ {
		steps = append(steps, silentCall(index))
	}
	steps = append(steps, endingStep())

	var ran atomic.Int64
	completer := &scriptedCompleter{steps: steps}
	agent := loopAgent(t, completer, runCounter("look", &ran))

	events, err := agent.Submit(context.Background(), "go")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if got := int(ran.Load()); got != rung {
		t.Fatalf("the tool ran %d times, want %d: the submission after the enforced rung was executed", got, rung)
	}
	held := heldToolResults(agent)
	if len(held) != 1 {
		t.Fatalf("held answers: got %d, want 1: %v", len(held), held)
	}
	if !strings.Contains(held[0], "Nothing was run this step") {
		t.Fatalf("the demand does not say the step was not run: %q", held[0])
	}
	// AND THE HELD CALL IS THE HARNESS'S OWN ANSWER, which is what keeps the loop
	// detector from reading a batch the harness refused as the model repeating
	// itself, and what freezes the silent ladder where it is.
	failed := false
	for _, event := range collected {
		if event.Kind == EventToolFailed && event.HarnessMade && strings.HasPrefix(event.Output, "[held]") {
			failed = true
		}
	}
	if !failed {
		t.Fatal("no harness-made tool failure carried the demand out to the surface")
	}
}

// And it lands honestly if it still refuses, rather than arguing forever.
func TestAModelThatKeepsRefusingLandsOnAnHonestLine(t *testing.T) {
	rung := enforcedRungAt()
	attempts := rung + processRuleRefusals + 1
	steps := make([]step, 0, attempts+1)
	for index := 0; index < attempts; index++ {
		steps = append(steps, silentCall(index))
	}
	// The script can answer more than the turn will ask for; a turn that asked
	// for this one would not have stopped.
	steps = append(steps, endingStep())

	var ran atomic.Int64
	completer := &scriptedCompleter{steps: steps}
	agent := loopAgent(t, completer, runCounter("look", &ran))

	events, err := agent.Submit(context.Background(), "go")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if completer.requests() != attempts {
		t.Fatalf("provider requests = %d, want %d: the turn did not stop on the refusal budget",
			completer.requests(), attempts)
	}
	if got, want := len(heldToolResults(agent)), processRuleRefusals; got != want {
		t.Fatalf("held answers: got %d, want %d", got, want)
	}
	landing := (writeNotesRule{}).stopped()
	notice, ok := firstOfKind(collected, EventNotice)
	if !ok || notice.Text != landing {
		t.Fatalf("landing notice = %q, present=%v, want %q", notice.Text, ok, landing)
	}
	if last := collected[len(collected)-1]; last.Kind != EventTurnDone {
		t.Fatalf("last event = %v, want EventTurnDone", last.Kind)
	}
	if got := int(ran.Load()); got != rung {
		t.Fatalf("the tool ran %d times, want %d", got, rung)
	}
	// AND THE TRANSCRIPT CLOSES. Every call the last submission named has an
	// answer beside it, or the next request is a shape no provider accepts.
	agent.mu.Lock()
	defer agent.mu.Unlock()
	calls, answers := 0, 0
	for _, message := range agent.messages {
		calls += len(message.ToolCalls)
		if message.Role == "tool" {
			answers++
		}
	}
	if calls != answers {
		t.Fatalf("%d tool calls left with %d answers; the transcript does not close", calls, answers)
	}
}

// A compliant model never sees the enforced rung, however long it works: a
// submission that carries a note beside its calls is never held, and the
// advisory ladder never climbs past nothing.
func TestACompliantModelNeverSeesTheEnforcedRung(t *testing.T) {
	rounds := 4 * enforcedRungAt()
	steps := make([]step, 0, rounds+1)
	for index := 0; index < rounds; index++ {
		steps = append(steps, spokenCall(index, fmt.Sprintf("checking file %d next, because the last one named it", index)))
	}
	steps = append(steps, endingStep())

	var ran atomic.Int64
	completer := &scriptedCompleter{steps: steps}
	agent := loopAgent(t, completer, runCounter("look", &ran))

	events, err := agent.Submit(context.Background(), "go")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if got := int(ran.Load()); got != rounds {
		t.Fatalf("the tool ran %d times, want %d: something was held from a model that wrote its notes", got, rounds)
	}
	if held := heldToolResults(agent); len(held) != 0 {
		t.Fatalf("a compliant model was held: %v", held)
	}
	if notes := silentTranscriptNotes(agent); len(notes) != 0 {
		t.Fatalf("a compliant model was advised: %v", notes)
	}
	landing := (writeNotesRule{}).stopped()
	for _, event := range collected {
		if event.Kind == EventNotice && event.Text == landing {
			t.Fatal("a compliant model's turn was stopped for its notes")
		}
	}
}

// One rung, and then back to normal: the note that lands after a hold puts the
// tools back on the same step, and the ladder starts again from the first rung.
func TestANoteAfterAHoldPutsTheToolsBack(t *testing.T) {
	rung := enforcedRungAt()
	after := silentStreakLimit - 1
	steps := make([]step, 0, rung+after+3)
	for index := 0; index <= rung; index++ {
		steps = append(steps, silentCall(index))
	}
	steps = append(steps, spokenCall(rung+1, "here is what I have found so far, and what I am checking next"))
	for index := 0; index < after; index++ {
		steps = append(steps, silentCall(rung+2+index))
	}
	steps = append(steps, endingStep())

	var ran atomic.Int64
	completer := &scriptedCompleter{steps: steps}
	agent := loopAgent(t, completer, runCounter("look", &ran))

	events, err := agent.Submit(context.Background(), "go")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	// Everything ran except the one held submission, and the ladder never
	// reached a second advisory after the note: the stretch that follows it is
	// shorter than the first rung.
	if got, want := int(ran.Load()), rung+1+after; got != want {
		t.Fatalf("the tool ran %d times, want %d: the hold did not lift on the note", got, want)
	}
	if got := len(heldToolResults(agent)); got != 1 {
		t.Fatalf("held answers: got %d, want exactly the one rung", got)
	}
	if got, want := len(silentTranscriptNotes(agent)), silentRungs; got != want {
		t.Fatalf("advisory notes: got %d, want %d", got, want)
	}
}

// The count of advisories per CONVERSATION is in the journal, which is the only
// place the thirty-two-ignores baseline can be measured from.
func TestTheProcessRuleCountPerConversationIsJournaled(t *testing.T) {
	rung := enforcedRungAt()
	steps := make([]step, 0, rung+2)
	for index := 0; index <= rung; index++ {
		steps = append(steps, silentCall(index))
	}
	steps = append(steps, endingStep())

	path := filepath.Join(t.TempDir(), "session.jsonl")
	var ran atomic.Int64
	completer := &scriptedCompleter{steps: steps}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = path
		config.AskConsent = false
	})
	agent.tools = append(agent.tools, runCounter("look", &ran))

	events, err := agent.Submit(context.Background(), "go")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	var advised, held []journalRule
	for _, entry := range journaledEntries(t, path, "rule") {
		if entry.Rule == nil {
			t.Fatal("a rule line carried no rule")
		}
		if entry.Rule.Rule != (writeNotesRule{}).slug() {
			t.Fatalf("rule line names %q", entry.Rule.Rule)
		}
		switch entry.Rule.Event {
		case ruleAdvised:
			advised = append(advised, *entry.Rule)
		case ruleHeld:
			held = append(held, *entry.Rule)
		default:
			t.Fatalf("unknown rule event %q", entry.Rule.Event)
		}
	}
	if len(advised) != silentRungs {
		t.Fatalf("advised lines: got %d, want %d", len(advised), silentRungs)
	}
	for index, line := range advised {
		if line.Count != index+1 {
			t.Fatalf("advisory %d carries count %d, want %d: the count is not the conversation's running total",
				index+1, line.Count, index+1)
		}
	}
	if len(held) != 1 || held[0].Count != silentRungs {
		t.Fatalf("held lines = %v, want one carrying the advisory count %d", held, silentRungs)
	}
}

// ── the mechanism itself ────────────────────────────────────────────────────

// The escalation is generic: it reads a rule's four answers and knows nothing
// about notes. This pins the contract every tenant has to keep, so the second
// rule added to the registry is a line in the slice and not a second mechanism.
func TestEveryEnforceableProcessRuleAnswersItsOwnQuestions(t *testing.T) {
	if len(enforceableProcessRules) == 0 {
		t.Fatal("the registry is empty; the loop enforces nothing")
	}
	seen := make(map[string]bool, len(enforceableProcessRules))
	for _, rule := range enforceableProcessRules {
		slug := rule.slug()
		if strings.TrimSpace(slug) == "" {
			t.Fatalf("%T has no slug; the journal cannot count it", rule)
		}
		if seen[slug] {
			t.Fatalf("two rules answer to %q; one count would be two counts", slug)
		}
		seen[slug] = true
		if len(rule.demand()) < 40 {
			t.Fatalf("%s has no demand a model can act on: %q", slug, rule.demand())
		}
		if len(rule.stopped()) < 20 {
			t.Fatalf("%s has no honest landing: %q", slug, rule.stopped())
		}
		// A rule that holds a turn on a watch that has said nothing would hold
		// every turn this program runs.
		if rule.ignored(newLoopWatch()) {
			t.Fatalf("%s enforces on a fresh turn", slug)
		}
		if rule.ignored(nil) {
			t.Fatalf("%s enforces on an episode with no watch", slug)
		}
		// NO MACHINERY VOCABULARY IN ANYTHING A PERSON READS (CLAUDE.md).
		for _, banned := range []string{"auditor", "verdict", "verified", "refuted", "predicate", "escalation", "registry"} {
			for _, said := range []string{rule.demand(), rule.stopped()} {
				if strings.Contains(strings.ToLower(said), banned) {
					t.Fatalf("%s says %q to a person: %q", slug, banned, said)
				}
			}
		}
	}
}

// The refusal budget is spent per rule and given back by compliance, which is
// the whole of "one escalation rung, then back to normal".
func TestTheRefusalBudgetIsSpentThenGivenBackByOneNote(t *testing.T) {
	watch := newLoopWatch()
	watch.silentRung = silentEnforceRung
	rules := newRuleWatch()
	silent := submission{calls: []ai.ToolCall{{ID: "a", Function: ai.ToolCallFunction{Name: "look"}}}}
	spoken := submission{calls: silent.calls, visibleText: true}

	for expected := 1; expected <= processRuleRefusals; expected++ {
		hold, held := rules.hold(watch, silent)
		if !held || hold.count != expected || hold.stop {
			t.Fatalf("refusal %d: held=%v count=%d stop=%v", expected, held, hold.count, hold.stop)
		}
	}
	if hold, held := rules.hold(watch, silent); !held || !hold.stop {
		t.Fatalf("the budget did not run out: held=%v stop=%v", held, hold.stop)
	}
	if _, held := rules.hold(watch, spoken); held {
		t.Fatal("a submission that met the rule was held")
	}
	if hold, held := rules.hold(watch, silent); !held || hold.count != 1 {
		t.Fatalf("the budget was not given back by the note: held=%v count=%d", held, hold.count)
	}
}

// The rung the enforcement reads is the rung the ladder has actually spoken, and
// nothing enforces before the advisory has been given twice.
func TestNothingIsEnforcedBeforeTheSecondAdvisory(t *testing.T) {
	watch := newLoopWatch()
	rule := writeNotesRule{}
	for index := 0; index < enforcedRungAt(); index++ {
		call := ai.ToolCall{ID: fmt.Sprintf("r%d", index), Function: ai.ToolCallFunction{
			Name: "look", Arguments: fmt.Sprintf(`{"path":"%d"}`, index)}}
		if rule.ignored(watch) {
			t.Fatalf("enforced after %d silent batches, before the second advisory", index)
		}
		watch.observe([]ai.ToolCall{call}, []toolResult{{text: fmt.Sprintf("file %d", index)}}, false)
	}
	if !rule.ignored(watch) {
		t.Fatalf("the rule is still advice after %d silent batches", enforcedRungAt())
	}
}

// ── the manual, which cannot interpolate a constant ─────────────────────────

// ONE SOURCE OF TRUTH, ENFORCED THE ONLY WAY MARKDOWN ALLOWS. The page a person
// reads has to spell the two numbers the loop applies, and a page that cannot
// interpolate them is a page that drifts the day either constant moves. So the
// numbers are asserted rather than interpolated, and the failure names them.
func TestTheManualSpellsTheEnforcedRungsOwnNumbers(t *testing.T) {
	page, ok := manual.Chat().Page("keys")
	if !ok {
		t.Fatal("the keys page is missing from the chat corpus")
	}
	for _, want := range []string{
		fmt.Sprintf("The `[silent]` note at %d replies is", silentThreshold(0)),
		fmt.Sprintf("The one at %d is the last", enforcedRungAt()),
		fmt.Sprintf("After %d held replies the turn stops", processRuleRefusals),
		(writeNotesRule{}).stopped(),
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the keys page does not say %q", want)
		}
	}
}
