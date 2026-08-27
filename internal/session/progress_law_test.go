package session

// THE NO-PROGRESS COUNTER, HELD TO ITS LAW (novelty.go, task_run.go).
//
// Line novelty judges ONE shape — the same question asked again of a deliverable
// that has not moved — and this file is the boundary of that jurisdiction, drawn
// from both sides:
//
//   - a real run that was killed inside it, replayed step for step;
//   - the loop it exists for, which must still die;
//   - the working cycle it was killing, which must not.
//
// Nothing here knows what kind of work is being done. The replay is data.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// ── the counter's own rule, driven ──────────────────────────────────────────

// replayStep is one finished tool call as [runTaskChild]'s drain sees it: the
// hand, what it was asked, what came back, and the two structural facts the
// runner reads off the worktree beside it.
type replayStep struct {
	Tool   string `json:"tool"`
	Args   string `json:"args"`
	Output string `json:"output"`
	// Saved is a successful call to a hand that puts a file on disk, Moved a
	// step that left the worktree different from how it found it. In a replay
	// they are false unless the journal shows the node writing.
	Saved bool `json:"-"`
	Moved bool `json:"-"`
}

// worstIdle runs steps through [addedSomething] — the counter's whole rule, in
// the one place the runner reads it — and gives back the LONGEST run of steps
// that added nothing. That number against [taskNoProgress] is the whole
// question: at six the node is landed.
func worstIdle(steps []replayStep) int {
	ledger := newProgressLedger()
	idle, worst := 0, 0
	for _, step := range steps {
		event := Event{Kind: EventToolEnd, Tool: step.Tool, Args: step.Args, Output: step.Output}
		if addedSomething(event, step.Saved, step.Moved, ledger) {
			idle = 0
			continue
		}
		idle++
		if idle > worst {
			worst = idle
		}
	}
	return worst
}

// worstIdleByLinesAlone is the ACCOUNTING AS IT WAS: line novelty and nothing
// else, with no idea whether the deliverable had moved or whether the question
// had ever been asked. It is here so the replay below can show the regression
// rather than assert that today's code agrees with itself.
func worstIdleByLinesAlone(steps []replayStep) int {
	seen := newLineNovelty()
	idle, worst := 0, 0
	for _, step := range steps {
		if step.Saved || step.Moved {
			idle = 0
			continue
		}
		if mostlyNew(seen.measure(step.Tool, stripJobFooter(step.Output))) {
			idle = 0
			continue
		}
		idle++
		if idle > worst {
			worst = idle
		}
	}
	return worst
}

// loadReplay reads a journal fixture: one finished call per line, in the order
// the run made them.
func loadReplay(t *testing.T, path string) []replayStep {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer file.Close()
	var steps []replayStep
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var step replayStep
		if err := json.Unmarshal([]byte(line), &step); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		steps = append(steps, step)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return steps
}

// ── (i) the run that was killed, replayed ───────────────────────────────────

// THE REGRESSION, AS IT HAPPENED, FROM THE JOURNAL.
//
// SWE-Marathon rust-java-lsp, seed 8, task 4 (2026-08-26T04:04:51Z): a worker
// read the server, built it, ran the scoring harness, and then spent six steps
// pulling a DIFFERENT LSP method's expected shapes out of the golden corpus —
// documentSymbol, foldingRange, hover, prepareCallHierarchy, semanticTokens,
// selectionRange. Every one of those answers was new to it. Every one of them
// was also mostly `    {`, `      "startLine": 1,` and `  }`, because that is
// what pretty-printed JSON is made of: 4% to 20% new lines. Six steps under the
// threshold, LAND NOW, nine minutes of work brought home mid-survey.
//
// THE NODE NEVER CALLED A SAVING HAND IN THE WHOLE RUN, which is why this
// fixture is the sharp one: no write and no worktree movement rescues it, so it
// is decided entirely by what the readings were worth.
//
// The fixture is the journal's tool calls and results in order, each result cut
// at [outputLimit] exactly as the display copy the counter reads is cut.
func TestTheSeedEightReplayIsNeverReadAsASpin(t *testing.T) {
	t.Parallel()
	steps := loadReplay(t, "testdata/s8-task4-steps.jsonl")
	if len(steps) < 25 {
		t.Fatalf("the replay holds %d steps, want the run's 25", len(steps))
	}
	for _, step := range steps {
		if step.Saved || step.Moved {
			t.Fatal("the fixture claims the node wrote something; the run never did")
		}
	}

	// THE ACCOUNTING AS IT WAS: six dead steps in a row, which is the kill.
	if was := worstIdleByLinesAlone(steps); was < taskNoProgress {
		t.Fatalf("the replay reaches %d idle steps under line novelty alone, "+
			"want at least %d — the fixture no longer holds the regression",
			was, taskNoProgress)
	}
	// AND AS IT IS.
	if worst := worstIdle(steps); worst >= taskNoProgress {
		t.Fatalf("the replay reaches %d idle steps, want fewer than %d", worst, taskNoProgress)
	}
}

// ── (ii) and the loop the counter exists for still dies ─────────────────────

// THE SAME QUESTION, ASKED AGAIN, OF WORK THAT HAS NOT MOVED. Six re-runs of one
// command whose only new line is its clock: nothing was written, nothing was
// asked that had not been asked, and fifteen lines in sixteen are ones the node
// already had. That is the shape the ratchet was built for and it must still
// close.
func TestSixReMeasurementsOfAnUnchangedDeliverableStillTrip(t *testing.T) {
	t.Parallel()
	steps := make([]replayStep, 0, 8)
	for round := 1; round <= 8; round++ {
		steps = append(steps, replayStep{
			Tool:   "bash",
			Args:   `{"command":"./measure.sh"}`,
			Output: strings.Join(sameRunNewClock(round), "\n"),
		})
	}
	if worst := worstIdle(steps); worst < taskNoProgress {
		t.Fatalf("eight re-measurements reached only %d idle steps, want %d",
			worst, taskNoProgress)
	}
}

// AND SO DOES THE POLL. Distinct commands every time — the arm that admits a
// question never asked before is one word from the activity detector lane/l
// replaced — but every answer is one the node has already been given, so nothing
// came back and nothing counts.
func TestDistinctQuestionsWithNothingNewInTheAnswersStillTrip(t *testing.T) {
	t.Parallel()
	steps := make([]replayStep, 0, 9)
	for poll := 1; poll <= 9; poll++ {
		steps = append(steps, replayStep{
			Tool:   "bash",
			Args:   fmt.Sprintf(`{"command":"sleep %d && tail jobs/1.log"}`, poll*15),
			Output: "(no output)",
		})
	}
	if worst := worstIdle(steps); worst < taskNoProgress {
		t.Fatalf("nine empty polls reached only %d idle steps, want %d", worst, taskNoProgress)
	}
}

// ── (iii) and the most legitimate loop there is never dies ──────────────────

// EDIT → BUILD → CHECK, THIRTY STEPS, AND NOTHING FIRES.
//
// This is the s9 shape, which is the one that made the law: the build reprints
// the same warnings it printed last time, and the check reprints a sixteen-row
// table in which three numbers moved. Neither reading is more new than old.
// Both are taken over a deliverable that changed a step ago, and a reading taken
// over a changed deliverable is information by construction — the node measured
// a state that did not exist before, and finding out that an edit moved little
// is finding something out.
func TestEditBuildCheckOverThirtyStepsNeverTrips(t *testing.T) {
	t.Parallel()
	warnings := make([]string, 0, 12)
	for warning := 1; warning <= 12; warning++ {
		warnings = append(warnings, fmt.Sprintf("warning: field `f%02d` is never read", warning))
	}
	steps := make([]replayStep, 0, 30)
	for round := 1; round <= 10; round++ {
		steps = append(steps, replayStep{
			Tool:   "edit",
			Args:   fmt.Sprintf(`{"path":"src/main.rs","edits":[{"oldText":"a%d"}]}`, round),
			Output: "edited src/main.rs",
			Saved:  true,
			Moved:  true,
		}, replayStep{
			Tool:   "bash",
			Args:   `{"command":"cargo build --release 2>&1 | tail -40"}`,
			Output: strings.Join(warnings, "\n"),
		}, replayStep{
			Tool:   "bash",
			Args:   `{"command":"bash run_tests.sh"}`,
			Output: strings.Join(movedByThree(round), "\n"),
		})
	}
	if worst := worstIdle(steps); worst >= taskNoProgress {
		t.Fatalf("a working cycle of thirty steps reached %d idle steps, want fewer than %d",
			worst, taskNoProgress)
	}

	// AND THE RESCUE IS THE CHANGE AND NOT THE ESTIMATOR. Read by line novelty
	// alone, both readings in this cycle are dead: the build reprints what it
	// printed last time and the check moves three rows in sixteen. The cycle
	// survives because they were taken over a deliverable that had just moved,
	// which is a fact about the work and not about the bytes.
	//
	// This is also why the cycle ALONE never reached six on its own: the edit
	// between the readings dirties the worktree, so the old accounting scored two
	// dead steps per round and not six. What it did was fill the counter two
	// thirds full on every honest round, so that any three further steps —
	// re-reading a file, asking the corpus one more question — landed the node.
	seen := newLineNovelty()
	for round := 1; round <= 3; round++ {
		seen.measure("bash", strings.Join(warnings, "\n"))
		seen.measure("bash", strings.Join(movedByThree(round), "\n"))
	}
	if mostlyNew(seen.measure("bash", strings.Join(warnings, "\n"))) {
		t.Fatal("a rebuild that reprinted its warnings read as more new than old")
	}
	if mostlyNew(seen.measure("bash", strings.Join(movedByThree(4), "\n"))) {
		t.Fatal("a check that moved three rows in sixteen read as more new than old")
	}
}

// movedByThree is a sixteen-row per-method table in which THREE rows carry a
// different number each round and thirteen do not: a check whose subject moved
// a little. Three of sixteen is 19%, well under [taughtLineThreshold], which is
// the whole point — the reading is not saved by the estimator, it is saved by
// having been taken over work that changed.
func movedByThree(round int) []string {
	rows := []string{"=== running 68186 golden points ==="}
	for row := 1; row <= 15; row++ {
		if row <= 3 {
			rows = append(rows, fmt.Sprintf("method %02d: %d passing", row, round*137))
			continue
		}
		rows = append(rows, fmt.Sprintf("method %02d: 0 passing", row))
	}
	return rows
}

// ── and the deliverable's change is spent, not held ─────────────────────────

// ONE READING PER CHANGE, AND THEN THE ESTIMATOR AGAIN. The free pass is "since
// the last informative result" and not "ever since the last edit": a node that
// writes once and then re-runs the same unchanged check forever is stopped, one
// step later than a node that never wrote at all.
func TestAChangeBuysOneReadingAndNotAnEndlessOne(t *testing.T) {
	t.Parallel()
	steps := []replayStep{{
		Tool: "write", Args: `{"path":"notes.md"}`, Output: "wrote notes.md",
		Saved: true, Moved: true,
	}}
	for round := 1; round <= 9; round++ {
		steps = append(steps, replayStep{
			Tool:   "bash",
			Args:   `{"command":"./measure.sh"}`,
			Output: strings.Join(sameRunNewClock(round), "\n"),
		})
	}
	if worst := worstIdle(steps); worst < taskNoProgress {
		t.Fatalf("one write bought %d idle steps of re-measuring, want the counter to reach %d",
			worst, taskNoProgress)
	}
}
