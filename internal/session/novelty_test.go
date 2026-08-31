package session

// Progress is information, and information is measured at the LINE (novelty.go).
//
// Every fixture here is synthetic: printed rows, numbered tables, boilerplate
// confirmations. Nothing in this file knows what kind of work is being done,
// which is the same law the estimator itself is held to.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── fixtures ────────────────────────────────────────────────────────────────

// sameRunNewClock is a sixteen-line result whose ONLY difference between runs is
// its first line: the shape of a script re-run against a deliverable that has
// not moved.
func sameRunNewClock(round int) []string {
	lines := []string{fmt.Sprintf("Starting at 2026-08-25T10:%02d:00Z", round)}
	for row := 1; row <= 15; row++ {
		lines = append(lines, fmt.Sprintf("row %02d ok", row))
	}
	return lines
}

// newTableEachTime is a sixteen-line result whose every row differs between
// runs: the shape of a measurement that is actually measuring something new.
func newTableEachTime(round int) []string {
	lines := []string{fmt.Sprintf("Starting at 2026-08-25T11:%02d:00Z", round)}
	for row := 1; row <= 15; row++ {
		lines = append(lines, fmt.Sprintf("row %02d value %d", row, round*1000+row))
	}
	return lines
}

// printing is a shell command that prints exactly these lines and touches
// nothing, so a node running it is learning or repeating and never writing.
func printing(lines []string) string {
	var out strings.Builder
	out.WriteString("printf '%s\\n'")
	for _, line := range lines {
		out.WriteString(" '" + line + "'")
	}
	return out.String()
}

// bashSteps scripts one child turn: one bash call per command, then an answer.
func bashSteps(commands []string) []step {
	steps := make([]step, 0, len(commands)+1)
	for index, command := range commands {
		id := fmt.Sprintf("call-%d", index)
		arguments, _ := json.Marshal(struct {
			Command string `json:"command"`
		}{Command: command})
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse(id, "bash", string(arguments)), nil
		})
	}
	return append(steps, finalText("done"))
}

// runScriptedNode runs one node whose child does exactly these steps and gives
// back the report it stopped with.
func runScriptedNode(t *testing.T, child []step, noProgress, maxSteps int) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	completer := &routedCompleter{
		parent: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				arguments, _ := json.Marshal(taskArguments{
					Title: "Measure", Summary: "s", Brief: "measure it\n" + taskBriefMark,
					Deliverable: "d", Acceptance: "a",
					NoProgress: noProgress, MaxSteps: maxSteps,
				})
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
	})
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "measure it"))
	node := graph.node(1)
	waitDoneNode(t, node)
	return node.notice().Report
}

// ── (i) the measure→measure loop ────────────────────────────────────────────

// ONE CHANGED LINE IN SIXTEEN IS NOT A DISCOVERY.
//
// The measured failure this whole file exists for: a worker re-ran its own
// measuring script against a deliverable it had stopped touching, and every run
// printed a fresh clock on its first line. A detector that hashed the result saw
// six discoveries; the score under it had not moved.
//
// THE COMMAND IS THE SAME EVERY ROUND, which is a correction to this fixture and
// not a weakening of it. The worker that was measured ran `./measure.sh` six
// times; the `--run %d` this test used to append made every call a question the
// node had never asked, which is a DIFFERENT shape — and one the counter now
// reads differently on purpose, because a question never asked before is not a
// re-measurement (novelty.go's [progressLedger]). Re-measurement is what this
// test is about, so re-measurement is what it scripts.
func TestAResultWhoseOnlyNewLineIsItsClockTeachesNothing(t *testing.T) {
	t.Parallel()
	seen := newProgressLedger()
	taught := 0
	for round := 1; round <= 6; round++ {
		event := Event{
			Kind:   EventToolEnd,
			Tool:   "bash",
			Args:   `{"command":"./measure.sh"}`,
			Output: strings.Join(sameRunNewClock(round), "\n"),
		}
		if taughtSomething(event, seen) {
			taught++
		}
	}
	// The first run is a real discovery — the node had never seen the table.
	// The other five are one new line each.
	if taught != 1 {
		t.Fatalf("%d of six re-runs taught something, want only the first", taught)
	}
}

// AND THE COUNTER ACTUALLY KILLS IT: the same six results through a real node
// and a real drain.
//
// ONE COMMAND STRING, TEN RUNS, A FRESH CLOCK EACH TIME. The counter is
// re-measured against the shape it exists for, so the script has to be the shape
// it exists for: the node calls the SAME thing every step and the world answers
// slightly differently, because the clock lives in the script and not in the
// call. The counter file is under $HOME rather than in the node's worktree —
// a node that dirtied its own tree every step would be making progress by the
// only rule that cannot be argued with ([worktreeMoved]), and this test would be
// measuring nothing.
//
// AND IT IS THE COUNTER'S OWN SENTENCE THAT IS ASSERTED. This used to read the
// loop guard's `going in circles` instead, which it reached only because the
// SILENT ladder raced the counter home: the same command with nothing said
// between the calls booked a nudge at six batches, and the third nudge ended the
// turn one step before the counter would have. Silence books nothing now
// (looped.go), so the road this test is named for is the road it takes.
func TestANodeThatRemeasuresAnUnchangedDeliverableIsStopped(t *testing.T) {
	report := runScriptedNode(t, bashSteps(repeated(remeasuring, 10)), 6, 40)
	if !strings.Contains(report, "6 steps without progress") {
		t.Fatalf("re-measuring an unchanged deliverable was not stopped by the counter: %q", report)
	}
}

// remeasuring is one fixed command whose answer carries a fresh first line and
// fifteen lines the node already has: the codeaf loop in a single string.
const remeasuring = `n=$(cat "$HOME/.runs" 2>/dev/null || echo 0); ` +
	`n=$((n+1)); echo "$n" > "$HOME/.runs"; ` +
	`echo "Starting at run $n"; ` +
	`for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do printf "row %02d ok\n" "$i"; done`

// repeated is one command, n times.
func repeated(command string, n int) []string {
	commands := make([]string, 0, n)
	for range n {
		commands = append(commands, command)
	}
	return commands
}

// ── (ii) and a real measurement is never punished ───────────────────────────

// SIX RESULTS THAT EACH CARRY A NEW TABLE ARE SIX DISCOVERIES. This is the
// failure the threshold has to stay clear of: an estimator that mistook new
// numbers for repetition would stop the node that was doing the work.
func TestSixNewTablesAreSixDiscoveries(t *testing.T) {
	t.Parallel()
	seen := newProgressLedger()
	for round := 1; round <= 6; round++ {
		event := Event{
			Kind:   EventToolEnd,
			Tool:   "bash",
			Args:   `{"command":"./measure.sh"}`,
			Output: strings.Join(newTableEachTime(round), "\n"),
		}
		if !taughtSomething(event, seen) {
			t.Fatalf("round %d of a moving measurement read as a spin", round)
		}
	}
}

func TestANodeMeasuringSomethingThatMovesIsNotStopped(t *testing.T) {
	commands := make([]string, 0, 6)
	for round := 1; round <= 6; round++ {
		commands = append(commands, printing(newTableEachTime(round)))
	}
	report := runScriptedNode(t, bashSteps(commands), 6, 40)
	if strings.Contains(report, "without progress") {
		t.Fatalf("six moving measurements were stopped as a spin: %q", report)
	}
}

// ── (iii) normalisation is trailing whitespace and nothing else ─────────────

func TestALineThatDiffersOnlyInTrailingWhitespaceIsNotNew(t *testing.T) {
	t.Parallel()
	seen := newLineNovelty()
	first := "row 01 ok\nrow 02 ok"
	if fresh, lines := seen.measure("bash", first); fresh != 2 || lines != 2 {
		t.Fatalf("first result measured %d new of %d, want 2 of 2", fresh, lines)
	}
	// The same two lines, padded and with a stray carriage return.
	if fresh, lines := seen.measure("bash", "row 01 ok   \nrow 02 ok\t\r"); fresh != 0 || lines != 2 {
		t.Fatalf("padded repeat measured %d new of %d, want 0 of 2", fresh, lines)
	}
	// LEADING whitespace is content: an indented line is a different line, and a
	// harness that folded it would be deciding which differences matter.
	if fresh, _ := seen.measure("bash", "  row 01 ok"); fresh != 1 {
		t.Fatal("an indented line read as one already seen")
	}
	// A result with nothing in it is not information.
	if mostlyNew(seen.measure("bash", "   \n\n")) {
		t.Fatal("an empty result counted as a discovery")
	}
	// And ONE new line is: a single line is the whole of what came back.
	if !mostlyNew(seen.measure("bash", "the build is broken")) {
		t.Fatal("a single new line did not count as information")
	}
}

// ── (iv) the world changing is information by construction ──────────────────

// A DELIVERABLE THAT CHANGED IS PROGRESS WHATEVER THE CONFIRMATION SAID. The
// tools that save a file answer in boilerplate — the same sentence, the same
// length, every time — and an estimator reading only results would call nine
// real revisions a spin.
func TestWritingTheDeliverableIsProgressThroughBoilerplate(t *testing.T) {
	child := make([]step, 0, 10)
	for round := 1; round <= 9; round++ {
		id := fmt.Sprintf("call-write-%d", round)
		arguments, _ := json.Marshal(struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}{Path: "notes.md", Content: fmt.Sprintf("deliverable revision %02d", round)})
		child = append(child, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse(id, "write", string(arguments)), nil
		})
	}
	report := runScriptedNode(t, append(child, finalText("done")), 6, 40)
	if strings.Contains(report, "without progress") {
		t.Fatalf("nine real revisions were stopped as a spin: %q", report)
	}
	// AND THE ESTIMATOR ITSELF NEVER CLAIMED THEM. The saving half of the belt
	// is counted one branch up, by the file it saved, and must not also be
	// spendable as knowledge.
	seen := newProgressLedger()
	for round := 1; round <= 3; round++ {
		if taughtSomething(Event{Kind: EventToolEnd, Tool: "write",
			Args: `{"path":"notes.md"}`, Output: "wrote notes.md"}, seen) {
			t.Fatal("a saving hand was counted as knowledge")
		}
	}
}

// ── (v) a harness-made result is neither ────────────────────────────────────

// A HAND THE HARNESS TOOK AWAY NEVER REACHED THE WORLD (withdrawn.go). Its
// answer is not a discovery, it is not a spin, and — the part this file adds —
// its bytes are not REMEMBERED, so a sentence written on this side of the wall
// can never make a later real result look like something already known.
func TestAHarnessMadeResultIsNeitherAndIsNotRemembered(t *testing.T) {
	t.Parallel()
	seen := newProgressLedger()
	refused := Event{
		Kind: EventToolEnd, Tool: "bash", Args: `{"command":"go test ./..."}`,
		Output:      "Unknown tool: bash\nthis hand is no longer on your belt",
		HarnessMade: true,
	}
	for retry := 1; retry <= 8; retry++ {
		if taughtSomething(refused, seen) {
			t.Fatalf("retry %d of a withdrawn hand counted as a discovery", retry)
		}
	}
	// The same words, this time from the world: still new, because the harness's
	// own refusals were never entered in the node's knowledge.
	real := refused
	real.HarnessMade = false
	if !taughtSomething(real, seen) {
		t.Fatal("the harness's own refusals were remembered as knowledge")
	}
}

// ── (vi) the fact, in words, in both places ─────────────────────────────────

// THE [stuck] NOTE CARRIES THE STRUCTURAL FACT. A model told only that it has
// repeated itself three times knows what it did; a model also told that the work
// has not changed since step 1 knows what the work did.
func TestTheStuckNoteSaysWhenTheWorkLastChanged(t *testing.T) {
	t.Parallel()
	watch := newLoopWatch()

	write := ai.ToolCall{ID: "w", Function: ai.ToolCallFunction{
		Name: "write", Arguments: `{"path":"notes.md","content":"first"}`}}
	if _, ok := watch.observe([]ai.ToolCall{write}, []toolResult{{text: "wrote notes.md"}}, false); ok {
		t.Fatal("one write nudged")
	}

	measure := ai.ToolCall{ID: "m", Function: ai.ToolCallFunction{
		Name: "bash", Arguments: `{"command":"./measure.sh"}`}}
	var note string
	for round := 1; round <= 4; round++ {
		result := toolResult{text: strings.Join(sameRunNewClock(round), "\n")}
		if looping, ok := watch.observe([]ai.ToolCall{measure}, []toolResult{result}, false); ok {
			note = nudgeNote(looping)
		}
	}
	if note == "" {
		t.Fatal("four identical calls produced no note")
	}
	if !strings.Contains(note, "The work has not changed since step 1") {
		t.Fatalf("note = %q, want the step the work last changed at", note)
	}
	if !strings.Contains(note, "results since") && !strings.Contains(note, "result since") {
		t.Fatalf("note = %q, want what the results since brought", note)
	}
	// Plain words only: the person-facing wording law.
	for _, banned := range []string{"auditor", "verdict", "verified"} {
		if strings.Contains(strings.ToLower(note), banned) {
			t.Fatalf("note = %q, which says %q", note, banned)
		}
	}
}

// AND THE DIGEST THE MARK'S READER SEES CARRIES IT TOO, on one line, beside the
// ledger that cannot say it.
func TestTheDigestSaysWhenTheWorkLastChanged(t *testing.T) {
	t.Parallel()
	// One measurement, then the write, then four re-runs of the same
	// measurement: everything counted "since" is a re-run, which is the loop.
	messages := []ai.Message{
		{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "m0",
			Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"./measure.sh"}`}}}},
		{Role: "tool", ToolCallID: "m0", Content: []ai.ContentPart{
			{Type: "text", Text: strings.Join(sameRunNewClock(0), "\n")}}},
		{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "w",
			Function: ai.ToolCallFunction{Name: "write", Arguments: `{"path":"notes.md","content":"first"}`}}}},
		{Role: "tool", ToolCallID: "w", Content: []ai.ContentPart{{Type: "text", Text: "wrote notes.md"}}},
	}
	for round := 1; round <= 4; round++ {
		id := fmt.Sprintf("m%d", round)
		messages = append(messages,
			ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: id,
				Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"./measure.sh"}`}}}},
			ai.Message{Role: "tool", ToolCallID: id, Content: []ai.ContentPart{
				{Type: "text", Text: strings.Join(sameRunNewClock(round), "\n")}}},
		)
	}
	digest := checkpointDigest("measure it", messages)
	if !strings.Contains(digest, checkpointDigestMoved) {
		t.Fatalf("digest has no moved section:\n%s", digest)
	}
	if !strings.Contains(digest, "work last changed: step 2") {
		t.Fatalf("digest does not say when the work last changed:\n%s", digest)
	}
	if !strings.Contains(digest, "results since: 4") {
		t.Fatalf("digest does not count the results since:\n%s", digest)
	}
	// Four re-runs of sixteen lines with one new clock line each: 4 of 64.
	if !strings.Contains(digest, "new lines since: 6%") {
		t.Fatalf("digest does not measure what those results brought:\n%s", digest)
	}
	// AN EMPTY TURN STILL SAYS NOTHING AT ALL.
	if got := checkpointDigest("", nil); got != "" {
		t.Fatalf("an empty turn produced a digest: %q", got)
	}
}

// ── (vii) and the ordinary way of working is never touched ──────────────────

// READ → EDIT → MEASURE, THIRTY STEPS, AND NOTHING FIRES. The measure step in
// this cycle is deliberately the non-informative one — the same table with a new
// clock — because that is what re-running a check looks like when it is the
// RIGHT thing to do: the work moved between the runs.
func TestALegitimateReadEditMeasureCycleNeverTrips(t *testing.T) {
	child := make([]step, 0, 31)
	for round := 1; round <= 10; round++ {
		read := printing([]string{
			fmt.Sprintf("part %d, line one", round),
			fmt.Sprintf("part %d, line two", round),
			fmt.Sprintf("part %d, line three", round),
		})
		arguments, _ := json.Marshal(struct {
			Command string `json:"command"`
		}{Command: read})
		id := fmt.Sprintf("read-%d", round)
		child = append(child, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse(id, "bash", string(arguments)), nil
		})

		wrote, _ := json.Marshal(struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}{Path: "notes.md", Content: fmt.Sprintf("revision %02d", round)})
		writeID := fmt.Sprintf("write-%d", round)
		child = append(child, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse(writeID, "write", string(wrote)), nil
		})

		check, _ := json.Marshal(struct {
			Command string `json:"command"`
		}{Command: printing(sameRunNewClock(round))})
		checkID := fmt.Sprintf("check-%d", round)
		child = append(child, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse(checkID, "bash", string(check)), nil
		})
	}
	report := runScriptedNode(t, append(child, finalText("done")), 6, 60)
	if strings.Contains(report, "without progress") {
		t.Fatalf("a working cycle of thirty steps was stopped: %q", report)
	}
}

// ── the memory has a ceiling ────────────────────────────────────────────────

// A NODE THAT READS A HUNDRED THOUSAND LINES DOES NOT KEEP A HUNDRED THOUSAND
// HASHES. Two generations, rotated, so the memory is between one and two
// generations and never more.
func TestTheLineMemoryStaysBounded(t *testing.T) {
	t.Parallel()
	seen := newLineNovelty()
	for line := 0; line < noveltyGeneration*5; line++ {
		seen.measure("bash", fmt.Sprintf("line %d", line))
	}
	if len(seen.recent)+len(seen.older) > 2*noveltyGeneration {
		t.Fatalf("the memory holds %d hashes, want at most %d",
			len(seen.recent)+len(seen.older), 2*noveltyGeneration)
	}
	// And what it just read is still remembered.
	if fresh, _ := seen.measure("bash", fmt.Sprintf("line %d", noveltyGeneration*5-1)); fresh != 0 {
		t.Fatal("the newest line was forgotten")
	}
}
