package session

// What the leash is told, and who decides on it.
//
// Every test here is written from ONE REAL RUN. On 2026-08-31 a cheap worker was
// handed a file to fix and rewrote it twenty-odd times in near-identical
// increments for twenty-two minutes and $7.99. Every call exited cleanly. The
// checkpoint that could have ended it was asked, was handed a list of successful
// `edit` calls, read that list as editing, and renewed the leash each time —
// until the step cap collected the run. Two things were missing and both are
// pinned below: the reader was never told whether anything had actually changed,
// and it was never made to look at the tree it was standing in.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// checkpointSpy stands in for the reader and keeps every list of evidence it was
// handed, because the evidence is the thing under test.
type checkpointSpy struct {
	mu      sync.Mutex
	asked   [][]string
	working bool
	reason  string
}

func (s *checkpointSpy) answer(string, []string) (bool, string) { return s.working, s.reason }

func (s *checkpointSpy) door() func(string, []string) (bool, string) {
	return func(brief string, evidence []string) (bool, string) {
		s.mu.Lock()
		s.asked = append(s.asked, append([]string(nil), evidence...))
		s.mu.Unlock()
		return s.answer(brief, evidence)
	}
}

func (s *checkpointSpy) rounds() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]string(nil), s.asked...)
}

// THE DOGFOOD COUNTERFACTUAL. A worker that rewrites one file with the same
// bytes is stopped by the leash long before its step budget, because the run of
// effects that changed nothing raises the checkpoint and the reader lands it.
// Before this the run was invisible: a successful save resets the no-progress
// counter, so nothing but the step cap stood between that worker and its budget.
func TestARewriteOfIdenticalContentLandsWellUnderTheStepCap(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	arguments, _ := json.Marshal(taskArguments{
		Title: "Rewrite", Summary: "s", Brief: "get the file right\n" + taskBriefMark,
		Deliverable: "d", Acceptance: "a", MaxSteps: 200, NoProgress: 3,
	})

	// Forty identical writes are far past anything this test expects to happen,
	// so a landing here is the leash ending the run rather than the script
	// running out of steps.
	var child []step
	for index := 0; index < 40; index++ {
		child = append(child, writeCall(fmt.Sprintf("call-%d", index), "note.md", "the same words\n"))
	}
	child = append(child, finalText("the active turn drained"), finalText("landed from what I had"))

	spy := &checkpointSpy{working: false, reason: "CIRCLING — note.md is what it was three writes ago"}
	completer := &routedCompleter{
		parent: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("call-task", "propose_task", string(arguments)), nil
			},
			finalText("handed off"),
		},
		child: child,
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskAudit = false
		config.TaskProgressCheck = spy.door()
	})
	collect(t, mustSubmit(t, agent, "fix the file"))

	node := agent.graph().node(1)
	waitDoneNode(t, node)
	notice := node.notice()
	if notice.State != TaskFailed {
		t.Fatalf("a node rewriting one file with the same bytes is %q, want failed (report %q)", notice.State, notice.Report)
	}
	if !strings.Contains(notice.Report, "repeat checkpoint") || !strings.Contains(notice.Report, "CIRCLING") {
		t.Fatalf("report = %q, want the repeat checkpoint and the reader's own words", notice.Report)
	}
	if rounds := spy.rounds(); len(rounds) != 1 {
		t.Fatalf("the reader was asked %d times, want exactly one — the first CIRCLING lands the work", len(rounds))
	}
	// AND IT HAPPENED EARLY. The budget was two hundred steps; a run of six
	// effects that changed nothing takes seven writes to make.
	completer.mu.Lock()
	spent := completer.seen.child
	completer.mu.Unlock()
	if spent > 20 {
		t.Fatalf("the node was asked %d times before it landed, want it stopped well under its 200-step budget", spent)
	}
}

// THE FACT IS IN FRONT OF THE READER, IN WORDS. The checkpoint used to be handed
// the hand and its arguments — a story about what the work set out to do — and a
// story of clean calls reads as work. It is now handed the count of actions in a
// row that produced nothing that was not already there, and every repeating
// action says so on its own line.
func TestTheCheckpointIsToldHowManyActionsProducedNothingNew(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	arguments, _ := json.Marshal(taskArguments{
		Title: "Rewrite", Summary: "s", Brief: "get the file right\n" + taskBriefMark,
		Deliverable: "d", Acceptance: "a", MaxSteps: 200, NoProgress: 3,
	})
	var child []step
	for index := 0; index < 12; index++ {
		child = append(child, writeCall(fmt.Sprintf("call-%d", index), "note.md", "the same words\n"))
	}
	child = append(child, finalText("the active turn drained"), finalText("landed from what I had"))

	spy := &checkpointSpy{working: false, reason: "CIRCLING — nothing has changed"}
	completer := &routedCompleter{
		parent: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("call-task", "propose_task", string(arguments)), nil
			},
			finalText("handed off"),
		},
		child: child,
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskAudit = false
		config.TaskProgressCheck = spy.door()
	})
	collect(t, mustSubmit(t, agent, "fix the file"))
	waitDoneNode(t, agent.graph().node(1))

	rounds := spy.rounds()
	if len(rounds) == 0 {
		t.Fatal("the reader was never asked about a run of writes that changed nothing")
	}
	evidence := strings.Join(rounds[0], "\n")
	if want := "the last 3 write calls produced byte-identical content"; !strings.Contains(evidence, want) {
		t.Fatalf("evidence = %q, want it to lead with %q", evidence, want)
	}
	if !strings.HasPrefix(rounds[0][0], "the last 3 write calls") {
		t.Fatalf("evidence leads with %q, want the fact in front of the story", rounds[0][0])
	}
	if !strings.Contains(evidence, "write {\"content\":\"the same words\\n\",\"path\":\"note.md\"} · "+repeatedEffectClause) &&
		!strings.Contains(evidence, "note.md") {
		t.Fatalf("evidence = %q, want the repeating action to say so on its own line", evidence)
	}
	if strings.Count(evidence, repeatedEffectClause) < 2 {
		t.Fatalf("evidence = %q, want the clause on the run and on the actions in it", evidence)
	}
}

// NOTHING IN THE HARNESS DECIDES THAT A REPEAT IS TOO MANY. The run raises the
// question and the reader answers it: told to carry on, a worker rewriting the
// same bytes forty times is not stopped by any repetition rule, and it is asked
// again rather than asked on every step.
func TestARunOfIdenticalEffectsEndsNothingByItself(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// TWO, so the reader is asked several times before the turn's own repetition
	// guard (looped.go) hands the turn over — which it will, because forty
	// identical calls are a loop by that rule as well. What is under test here is
	// that NO rule in the leash ends the work on the count itself.
	arguments, _ := json.Marshal(taskArguments{
		Title: "Rewrite", Summary: "s", Brief: "get the file right\n" + taskBriefMark,
		Deliverable: "d", Acceptance: "a", MaxSteps: 200, NoProgress: 2,
	})
	var child []step
	for index := 0; index < 40; index++ {
		child = append(child, writeCall(fmt.Sprintf("call-%d", index), "note.md", "the same words\n"))
	}
	child = append(child, finalText("note.md holds what was asked for"))

	spy := &checkpointSpy{working: true, reason: "WORKING — this is the deliverable being settled"}
	completer := &routedCompleter{
		parent: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("call-task", "propose_task", string(arguments)), nil
			},
			finalText("handed off"),
		},
		child: child,
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskAudit = false
		config.TaskProgressCheck = spy.door()
	})
	collect(t, mustSubmit(t, agent, "fix the file"))
	node := agent.graph().node(1)
	waitDoneNode(t, node)

	if report := node.notice().Report; strings.Contains(report, "repeat checkpoint") {
		t.Fatalf("report = %q, want a node the reader allowed to carry on to be left alone", report)
	}
	rounds := spy.rounds()
	if len(rounds) < 2 {
		t.Fatalf("the reader was asked %d times over a long run of repeats, want it asked again after each run it allowed", len(rounds))
	}
}

// THE READER HAS TO OPEN THE TREE IT IS STANDING IN. "Read the working copy if
// useful" is a suggestion, and a reader that skips it is answering a question
// about the world from a list of attempted calls — where the benefit of the
// doubt always goes to WORKING. An answer given without a single look is sent
// back once, and the second answer is the one that counts.
func TestTheProgressCheckIsSentBackWhenItNeverOpenedTheWorkingCopy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	reader := &groundReader{}
	agent, workspace := newTestAgent(t, reader, func(config *Config) {
		config.AskConsent = false
		config.TaskAudit = false
	})
	graph := stubbedGraph(agent, func(*TaskNode) {})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "t", brief: "get the file right", acceptance: "a"})

	working, reason := agent.taskProgress(context.Background(), graph.node(id), workspace,
		[]string{"write {\"path\":\"note.md\"} · " + repeatedEffectClause}, io.Discard)
	if working {
		t.Fatalf("the second answer did not stand: working=%v reason=%q", working, reason)
	}
	if !strings.Contains(reason, "CIRCLING") {
		t.Fatalf("reason = %q, want the answer given after the tree was opened", reason)
	}

	asked := reader.questions()
	if len(asked) != 2 {
		t.Fatalf("the reader was asked %d times, want a second ask after it answered without looking", len(asked))
	}
	if !strings.Contains(asked[0], "Read the working copy before you answer") {
		t.Fatalf("the charter does not require the read: %q", asked[0])
	}
	if !strings.Contains(asked[0], workspace) {
		t.Fatalf("the question does not name the ground the work is running in: %q", asked[0])
	}
	if !strings.Contains(asked[1], "You answered without reading the working copy") {
		t.Fatalf("the second ask does not say what was wrong with the first: %q", asked[1])
	}
}

// groundReader answers every WORKING-or-CIRCLING question put to it and keeps
// what it was asked, so a test can see whether the reader was made to look at
// the tree. It answers everything else — the harness names a fresh session on
// the side — with a word nobody reads.
type groundReader struct {
	mu    sync.Mutex
	asked []string
}

func (g *groundReader) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	text := strings.Join(messageTexts(messages), "\n")
	if !strings.Contains(text, "WORKING or CIRCLING first") || strings.Contains(text, "Name this session") {
		return textResponse("aside"), nil
	}
	g.mu.Lock()
	g.asked = append(g.asked, text)
	round := len(g.asked)
	g.mu.Unlock()
	if round == 1 {
		// Answered off the list of calls alone, without opening anything.
		return textResponse("WORKING — the calls all look fine"), nil
	}
	return textResponse("CIRCLING — note.md has not changed"), nil
}

func (g *groundReader) questions() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.asked...)
}

// AN EFFECT IS WHAT LANDED, NOT THE SENTENCE ABOUT IT. A saving hand answers
// with a line of prose whose byte count moves with the file, so two writes that
// leave identical bytes can answer differently. The bytes are what the brief is
// about, so the bytes are what is fingerprinted.
func TestAnEffectIsTheFileAndNotTheAnswerAboutIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("the same words\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	first, ok := effectPrintOf(Event{Kind: EventToolEnd, Tool: "write", Output: "wrote 15 bytes"}, dir, "note.md", true, false, "")
	if !ok {
		t.Fatal("a saving call over a real file produced no fingerprint")
	}
	second, ok := effectPrintOf(Event{Kind: EventToolEnd, Tool: "edit", Output: "replaced 1 occurrence in note.md"}, dir, "note.md", true, false, "")
	if !ok || first != second {
		t.Fatalf("two hands leaving identical bytes fingerprinted %d and %d, want one effect", first, second)
	}

	// And a file that really changed is a different effect.
	if err := os.WriteFile(path, []byte("other words\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	third, _ := effectPrintOf(Event{Kind: EventToolEnd, Tool: "write", Output: "wrote 15 bytes"}, dir, "note.md", true, false, "")
	if third == first {
		t.Fatal("a file whose content changed fingerprinted as the effect it used to be")
	}

	// AND A COMMAND THAT SAYS NOTHING WHILE LEAVING A FILE BEHIND IS JUDGED ON
	// THE TREE. `printf … > note-3.txt` answers with the same empty output every
	// time and writes a different file on every call, so the state of the tree —
	// not the answer — is what its effect is.
	quiet := Event{Kind: EventToolEnd, Tool: "bash", Output: "(no output)"}
	one, ok := effectPrintOf(quiet, dir, "", false, true, "?? note-1.txt")
	if !ok {
		t.Fatal("a command that moved the tree produced no fingerprint")
	}
	two, _ := effectPrintOf(quiet, dir, "", false, true, "?? note-1.txt\n?? note-2.txt")
	if one == two {
		t.Fatal("two commands that left different trees fingerprinted as one effect")
	}
	still, _ := effectPrintOf(quiet, dir, "", false, false, "?? note-1.txt")
	if still == one {
		t.Fatal("a command that changed nothing was fingerprinted by the tree it did not move")
	}

	// A hand the harness itself answered produced nothing at all, so it can
	// neither repeat an effect nor stand as one.
	if _, ok := effectPrintOf(Event{Kind: EventToolFailed, Tool: "bash", Output: "bash was withdrawn", HarnessMade: true}, dir, "", false, false, ""); ok {
		t.Fatal("a harness-made answer was recorded as something the node produced")
	}
}

// THE LEDGER STATES AND DOES NOT RULE, and the run it counts is the progress
// that wasn't: steps the no-progress counter read as progress whose effect was
// one already produced. Any other step ends it, which is what keeps this out of
// the counter's way on every shape of work the counter already stops.
func TestTheEffectLedgerCountsARunAndSaysIt(t *testing.T) {
	ledger := newEffectLedger()
	if ledger.saw("write", 1, true) {
		t.Fatal("the first effect of its kind is not a repeat")
	}
	ledger.counted("write", true, false)
	for round := 0; round < 3; round++ {
		if !ledger.saw("write", 1, true) {
			t.Fatalf("round %d: an effect already produced read as new", round)
		}
		ledger.counted("write", true, true)
	}
	if ledger.sameEffectRun() != 3 {
		t.Fatalf("run = %d, want three counted-as-progress repeats in a row", ledger.sameEffectRun())
	}
	if want := "the last 3 write calls " + repeatedEffectClause; ledger.sameEffectLine() != want {
		t.Fatalf("sentence = %q, want %q", ledger.sameEffectLine(), want)
	}

	// A mixed run says the general thing, which is still true.
	ledger.saw("edit", 1, true)
	ledger.counted("edit", true, true)
	if want := "the last 4 calls " + repeatedEffectClause; ledger.sameEffectLine() != want {
		t.Fatalf("sentence = %q, want %q", ledger.sameEffectLine(), want)
	}

	// A STEP THE COUNTER DID NOT READ AS PROGRESS ENDS THE RUN, because that step
	// is one the counter is already counting and this must not answer for it.
	ledger.saw("read", 1, true)
	ledger.counted("read", false, true)
	if ledger.sameEffectRun() != 0 {
		t.Fatalf("run = %d after a step the counter is already counting, want it ended", ledger.sameEffectRun())
	}

	// A genuinely new effect ends the run outright, and one repeat has nothing to
	// count: the action's own evidence line already carries it.
	if repeat := ledger.saw("write", 2, true); repeat {
		t.Fatal("an effect never produced before read as a repeat")
	}
	ledger.counted("write", true, false)
	if ledger.sameEffectRun() != 0 || ledger.sameEffectLine() != "" {
		t.Fatalf("a new effect left run=%d line=%q, want the run ended", ledger.sameEffectRun(), ledger.sameEffectLine())
	}
	ledger.saw("write", 2, true)
	ledger.counted("write", true, true)
	if ledger.sameEffectLine() != "" {
		t.Fatalf("one repeat says %q, want nothing — the action's own line already carries it", ledger.sameEffectLine())
	}

	// A step that produced nothing at all is never remembered, so it can never
	// make a later real effect look like something already produced.
	if ledger.saw("bash", 0, false) {
		t.Fatal("a step that produced nothing answered as a repeat")
	}

	// And a look that allows the run starts the count again without forgetting
	// what was produced.
	ledger.pardon()
	if ledger.sameEffectRun() != 0 {
		t.Fatalf("run = %d after it was allowed, want it started again", ledger.sameEffectRun())
	}
	if !ledger.saw("write", 2, true) {
		t.Fatal("an effect produced before the look read as new after it")
	}
}

// NO NUMBER IN THIS HARNESS SAYS HOW MANY REPEATS ARE TOO MANY. The ledger
// measures nothing against a constant, and the one place the leash reads the run
// borrows the node's own `no_progress` threshold — which is on the wire, so a
// brief whose work is legitimately repetitive can raise it — to decide when to
// ASK. What the run means is answered by whoever is asked.
func TestNoRepetitionConstantRulesTheLeash(t *testing.T) {
	ledger, err := os.ReadFile("effects.go")
	if err != nil {
		t.Fatalf("read effects.go: %v", err)
	}
	constant := regexp.MustCompile(`(?i)(run|streak|repeat)[A-Za-z]*\)?\s*[<>]=?\s*\d`)
	if found := constant.FindString(string(ledger)); found != "" {
		t.Fatalf("effects.go weighs repetition against a constant: %q", found)
	}

	// The leash is [childRun.trip] and its file is where a node's own run lives
	// (task_child_run.go). Naming the file rather than walking the package is
	// deliberate: `sameEffectRun` is declared in effects.go and answered by its
	// own tests, and a walk would count those as readings of the run.
	leash, err := os.ReadFile("task_child_run.go")
	if err != nil {
		t.Fatalf("read task_child_run.go: %v", err)
	}
	read := ""
	for _, line := range strings.Split(string(leash), "\n") {
		if strings.Contains(line, "sameEffectRun()") {
			if read != "" {
				t.Fatalf("the run is read in more than one place: %q and %q", read, line)
			}
			read = line
		}
	}
	if read == "" {
		t.Fatal("nothing in the leash reads the run of effects that changed nothing")
	}
	if !strings.Contains(read, "limits.noProgress") {
		t.Fatalf("the leash reads the run against %q, want the node's own no_progress threshold", strings.TrimSpace(read))
	}
}
