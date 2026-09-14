package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func askTestAgent(t *testing.T, interactive bool) *Agent {
	t.Helper()
	root := t.TempDir()
	a, err := newAgent(Config{Workspace: root, Place: Place{Dir: root, Workspace: root}, Model: "test/model", Interactive: interactive}, &scriptedCompleter{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func TestAskToolRoundTripsTheWholeAnswer(t *testing.T) {
	a := askTestAgent(t, true)
	raw := json.RawMessage(`{"head":"Which format?","kind":"choice","reason":"both formats fit and the record does not choose","options":[{"key":"1","label":"json"},{"key":"2","label":"yaml"}],"pick":{"key":"1","reason":"existing readers use it","confidence":"high"},"stakes":"reversible"}`)
	done := make(chan string, 1)
	go func() {
		text, _, _ := a.executeAsk(context.Background(), raw)
		done <- text
	}()
	var q Question
	deadline := time.After(time.Second)
	for {
		open := a.OpenQuestions()
		if len(open) > 0 {
			q = open[0]
			break
		}
		select {
		case <-deadline:
			t.Fatal("ask did not open")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	want := Answer{Kind: QuestionAsk, ID: q.ID, Key: "2", Picked: []string{"2"}, Change: "but keep comments", Comments: map[string]string{"2": "portable"}, Reframe: "choose for readers", AskedBack: []Exchange{{Option: "2", Asked: "why?", Replied: "portable"}}}
	if err := a.ResolveQuestion(want); err != nil {
		t.Fatal(err)
	}
	got := askAnswerRead(t, <-done)
	if got.Change != want.Change || got.Reframe != want.Reframe || len(got.AskedBack) != 1 || got.Comments["2"] != "portable" {
		t.Fatalf("answer did not round trip: %+v", got)
	}
}

func TestAskToolReturnsTheQuestionGatesRefusal(t *testing.T) {
	a := askTestAgent(t, true)
	text, failed, err := a.executeAsk(context.Background(), json.RawMessage(`{"head":"Which?","kind":"choice","options":[{"key":"1","label":"one"},{"key":"2","label":"two"}],"stakes":"reversible"}`))
	if err != nil || failed || !strings.Contains(text, "needs a reason") {
		t.Fatalf("refusal = %q, failed=%v, err=%v", text, failed, err)
	}
}

func TestAssumptionsStandAfterTheClock(t *testing.T) {
	a := askTestAgent(t, true)
	if err := a.SetAutonomy(AskAssumption, Policy{Kind: PolicyRecommendThenAuto, After: time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	text, failed, err := a.executeAsk(context.Background(), json.RawMessage(`{"head":"May I proceed on these assumptions?","kind":"assumption","reason":"the work is reversible and these facts are not in the record","options":[{"key":"1","label":"local only"},{"key":"2","label":"keep compatibility"}],"stakes":"reversible"}`))
	if err != nil || failed {
		t.Fatalf("ask failed: %q %v %v", text, failed, err)
	}
	answer := askAnswerRead(t, text)
	if strings.Join(answer.Picked, ",") != "1,2" || answer.DecidedBy != DecidedByDial {
		t.Fatalf("assumptions did not stand: %+v", answer)
	}
}

func TestAutonomyPersistsPerProjectAndFillsPolicy(t *testing.T) {
	root := t.TempDir()
	a := &Agent{config: Config{Workspace: root}}
	want := Policy{Kind: PolicyRecommendThenAuto, After: 3 * time.Minute}
	if err := a.SetAutonomy(AskChoice, want); err != nil {
		t.Fatal(err)
	}
	b := &Agent{config: Config{Workspace: root}}
	if got := b.autonomyFor(AskChoice); got != want {
		t.Fatalf("policy = %+v, want %+v", got, want)
	}
	if err := b.SetAutonomy(AskClarification, Policy{Kind: PolicyDecide}); err == nil {
		t.Fatal("clarification accepted an automatic policy")
	}
	// CONFIRMATION ALWAYS ASKS, refused at the same door and for the harder
	// reason: it is what is asked before something destructive, so a rule that
	// answered it would be a don't-ask-me-again on exactly the questions
	// stop.go's law says may never have one.
	if err := b.SetAutonomy(AskConfirmation, Policy{Kind: PolicyDecide}); err == nil {
		t.Fatal("confirmation accepted a rule that answers in the person's place")
	}
	// And a file edited by hand cannot make either of them run on a clock,
	// because the READ has the same floor as the write.
	if err := os.WriteFile(filepath.Join(root, ".codeaf", "autonomy.json"),
		[]byte(`{"confirmation":{"kind":"decide"},"clarification":{"kind":"decide"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &Agent{config: Config{Workspace: root}}
	if got := c.autonomyFor(AskConfirmation); got.Kind != PolicyAsk {
		t.Fatalf("a hand-written confirmation rule was honoured: %+v", got)
	}
	if got := c.autonomyFor(AskClarification); got.Kind != PolicyAsk {
		t.Fatalf("a hand-written clarification rule was honoured: %+v", got)
	}
}

// THE RECORD REACHES THE MODEL, and answering does not re-price the
// conversation to put it there. A decision is written to the file the gate
// reads, and the copy message[0] carries is brought up to date the next time
// that message is rebuilt for a reason of its own.
func TestTheRecordRidesInModelContext(t *testing.T) {
	a := askTestAgent(t, true)
	a.recordDecision(DecisionRecord{Head: "Which format?", Labels: []string{"json"}, By: DecidedByPerson, At: time.Now()})
	a.mu.Lock()
	a.standingText = "\n\nstanding orders\n- ship on Fridays"
	a.refreshSystemLocked()
	got := a.messages[0].Content[0].Text
	a.mu.Unlock()
	if !strings.Contains(got, "the record\n- Which format? → json") {
		t.Fatalf("system context has no record: %q", got)
	}
	// AND THE GATE READS THE FILE ITSELF, which is what the record is for: it
	// answers the same question without anybody having rebuilt anything.
	again := Question{Head: "Which format?", Reason: "it is not settled", Stakes: StakesReversible,
		Ask: AskChoice, Options: []AnswerOption{{Key: "1", Label: "json"}, {Key: "2", Label: "yaml"}}}
	if err := again.Check(a.Decisions()); err == nil {
		t.Fatal("the gate let a settled question through")
	}
}

func TestAnExplainedOverrideBecomesAPreferenceThatCanBeForgotten(t *testing.T) {
	a, _ := brainAgent(t, &scriptedCompleter{}, func(config *Config) { config.Interactive = true })
	raw := json.RawMessage(`{"head":"Which report style?","kind":"choice","reason":"the record has no report style","options":[{"key":"1","label":"long"},{"key":"2","label":"compact"}],"pick":{"key":"1","reason":"it carries more detail"},"stakes":"reversible"}`)
	done := make(chan struct{})
	go func() { _, _, _ = a.executeAsk(context.Background(), raw); close(done) }()
	var q Question
	deadline := time.After(time.Second)
	for len(a.OpenQuestions()) == 0 {
		select {
		case <-deadline:
			t.Fatal("ask did not open")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	q = a.OpenQuestions()[0]
	if err := a.ResolveQuestion(Answer{Kind: QuestionAsk, ID: q.ID, Key: "2", Why: "compact reports are easier for me to scan"}); err != nil {
		t.Fatal(err)
	}
	<-done
	// THE PREFERENCE IS WRITTEN BESIDE THE ANSWER AND NEVER IN FRONT OF IT
	// (question.go's [Agent.rememberOverride]): the answer's own door returns
	// the moment the lane has it, and the memory lands a moment later.
	var memories []MemoryLine
	var err error
	deadline = time.After(5 * time.Second)
	for {
		memories, err = a.Memories("compact reports")
		if err == nil && len(memories) > 0 && strings.Contains(memories[0].Text, "compact reports are easier") {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("preference was not kept: %+v, %v", memories, err)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	if _, err := a.Forget("compact reports"); err != nil {
		t.Fatal(err)
	}
	memories, _ = a.Memories("compact reports")
	if len(memories) != 0 {
		t.Fatalf("preference was not forgettable: %+v", memories)
	}
}

// THE KEY GRAMMAR IS THE SURFACE'S AND NOT THE ASKER'S. A model that names its
// answers `landscape` and `still-life` gets them renumbered `1`, `2`, … for the
// person, and reads its answer back in the names it wrote, with the labels.
func TestAnAskWithWordKeysIsRenumberedAndAnsweredInTheAskersOwnKeys(t *testing.T) {
	options := []AnswerOption{{Key: "landscape", Label: "Landscapes & seascapes"}, {Key: "still-life", Label: "Still life"}, {Key: "abstract", Label: "Abstract"}}
	pick := &Pick{Key: "still-life"}
	theirs := askDigitKeys(options, pick)
	for i, want := range []string{"1", "2", "3"} {
		if options[i].Key != want {
			t.Fatalf("option %d is keyed %q, want %q", i, options[i].Key, want)
		}
	}
	if pick.Key != "2" {
		t.Fatalf("the pick was not renumbered with its option · %q", pick.Key)
	}
	q := Question{Options: options}
	answer := askInTheirKeys(Answer{Key: "3", Picked: []string{"3", "1"}}, q, theirs)
	if answer.Key != "abstract" || strings.Join(answer.Picked, ",") != "abstract,landscape" {
		t.Fatalf("the asker reads %q / %v, want its own names", answer.Key, answer.Picked)
	}
	if strings.Join(answer.Labels, "|") != "Abstract|Landscapes & seascapes" {
		t.Fatalf("the labels do not ride beside the keys · %v", answer.Labels)
	}
	// AND KEYS THAT FOLLOWED THE GRAMMAR ARE LEFT EXACTLY AS THEY CAME.
	plain := []AnswerOption{{Key: "1", Label: "yes"}, {Key: "2", Label: "no"}}
	if theirs := askDigitKeys(plain, nil); theirs != nil {
		t.Fatalf("digit keys were renumbered · %v", theirs)
	}
	if got := askInTheirKeys(Answer{Key: "2", Picked: []string{"2"}}, Question{Options: plain}, nil); got.Key != "2" || got.Labels[0] != "no" {
		t.Fatalf("a plain answer came back changed · %+v", got)
	}
}

// A pick that names nothing is no pick — the model declining to recommend —
// and the question opens; refusing it sent a well-formed checklist of eight
// round for a comma on 2026-09-10.
func TestAPickWithNoKeyIsNoPickAndTheQuestionOpens(t *testing.T) {
	a := askTestAgent(t, true)
	raw := json.RawMessage(`{"head":"Which genres?","kind":"choice","reason":"their taste decides it","stakes":"reversible","input":{"kind":"checklist"},"options":[{"key":"1","label":"landscapes"},{"key":"2","label":"portraits"}],"pick":{"key":"","reason":"their taste decides it"}}`)
	done := make(chan string, 1)
	go func() {
		text, _, _ := a.executeAsk(context.Background(), raw)
		done <- text
	}()
	deadline := time.After(time.Second)
	for len(a.OpenQuestions()) == 0 {
		select {
		case text := <-done:
			t.Fatalf("the ask came back instead of opening: %q", text)
		case <-deadline:
			t.Fatal("ask did not open")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	q := a.OpenQuestions()[0]
	if q.Pick != nil {
		t.Fatalf("an empty pick was kept: %+v", q.Pick)
	}
	if err := a.ResolveQuestion(Answer{Kind: QuestionAsk, ID: q.ID, Key: "1", Picked: []string{"1"}}); err != nil {
		t.Fatal(err)
	}
	<-done
}

// A refusal says that nothing was shown, and the checkpoint reads that off the
// transcript: an `ask` refused and never made again is sent back once with the
// refusal in front of it.
func TestARefusedAskSaysNothingWasShownAndTheTurnIsSentBackOnce(t *testing.T) {
	a := askTestAgent(t, true)
	text, _, _ := a.executeAsk(context.Background(), json.RawMessage(`{"head":"Which?","kind":"choice","options":[{"key":"1","label":"one"},{"key":"2","label":"two"}],"stakes":"reversible"}`))
	if !strings.HasPrefix(text, askRefusedLead) {
		t.Fatalf("the refusal does not say nothing was shown: %q", text)
	}
	messages := []ai.Message{
		textMessage("user", "ask me which"),
		{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "c1", Function: ai.ToolCallFunction{Name: "ask", Arguments: "{}"}}}},
		{Role: "tool", ToolCallID: "c1", Content: []ai.ContentPart{{Text: text}}},
		textMessage("assistant", "The form is presented above."),
	}
	refusal, ok := askRefusedAndNotRetried(messages)
	if !ok || !strings.Contains(refusal, "needs a reason") || strings.HasPrefix(refusal, askRefusedLead) {
		t.Fatalf("the refused ask was not read back: ok=%v %q", ok, refusal)
	}
	if !strings.Contains(checkpointAskNudgeLead(refusal), "needs a reason") {
		t.Fatal("the nudge does not carry the refusal")
	}
	// An ask made after the refusal, answered, is the shape that is left alone.
	answered := append(messages,
		ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "c2", Function: ai.ToolCallFunction{Name: "ask", Arguments: "{}"}}}},
		ai.Message{Role: "tool", ToolCallID: "c2", Content: []ai.ContentPart{{Text: `{"kind":"ask","key":"1"}`}}})
	if _, ok := askRefusedAndNotRetried(answered); ok {
		t.Fatal("an ask that was made again and answered was nudged")
	}
}
