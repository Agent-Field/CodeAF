package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	var got Answer
	if err := json.Unmarshal([]byte(<-done), &got); err != nil {
		t.Fatal(err)
	}
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
	var answer Answer
	if err := json.Unmarshal([]byte(text), &answer); err != nil {
		t.Fatal(err)
	}
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
	if err := os.WriteFile(filepath.Join(root, ".aforge", "autonomy.json"),
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

func TestTheRecordRidesInModelContext(t *testing.T) {
	a := askTestAgent(t, true)
	a.recordDecision(DecisionRecord{Head: "Which format?", Labels: []string{"json"}, By: DecidedByPerson, At: time.Now()})
	a.mu.Lock()
	got := a.messages[0].Content[0].Text
	a.mu.Unlock()
	if !strings.Contains(got, "the record\n- Which format? → json") {
		t.Fatalf("system context has no record: %q", got)
	}
}

func TestAnExplainedOverrideBecomesAForgettablePreference(t *testing.T) {
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
	memories, err := a.Memories("compact reports")
	if err != nil || len(memories) == 0 || !strings.Contains(memories[0].Text, "compact reports are easier") {
		t.Fatalf("preference was not kept: %+v, %v", memories, err)
	}
	if _, err := a.Forget("compact reports"); err != nil {
		t.Fatal(err)
	}
	memories, _ = a.Memories("compact reports")
	if len(memories) != 0 {
		t.Fatalf("preference was not forgettable: %+v", memories)
	}
}
