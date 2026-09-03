package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/calllog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	homepkg "github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/router"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The whole point of `do` in one test: a headless run is not the static
// pipeline. A deliverable that a reviewer rejects — with the reviewer quoting
// the user's own words for what is missing — does not ship as written. The job
// grows the work that closes the gap, that work runs, and what the caller
// finally reads on stdout is the repaired answer.
//
// Every mechanism here is the resident's: the compiler that turns the ask into
// a goal, the working method written for a task-scale job, the executor, the
// delivery gate, the revision pass, the citation invariant, and the replan that
// splices new work into a live graph. None of it is reachable from a graph that
// was planned once and written to a file, which is exactly why `do` exists.
func TestDoRepairsARejectedDeliverableThroughTheGate(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task:      "write the release note and include the migration steps",
		timeout:   60 * time.Second,
		stdout:    &stdout,
		stderr:    &stderr,
		newClient: script.client,
	})
	if err != nil {
		t.Fatalf("the errand did not settle cleanly: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	// The gate ran on the first draft, again on the revision, and once more on
	// the work the gap bought. Two is the old ceiling; the third is the whole
	// mechanism under test.
	if got := script.count("gate"); got < 3 {
		t.Fatalf("delivery gate ran %d times, want at least 3 (draft, revision, repair)", got)
	}
	if got := script.count("extension"); got != 1 {
		t.Fatalf("the extension leaf ran %d times, want exactly 1", got)
	}
	if got := script.count("contract"); got == 0 {
		t.Fatal("no working method was written — a task-scale job must still get its contract")
	}
	if got := script.count("replan"); got == 0 {
		t.Fatal("the gap never reached the remainder planner")
	}
	// The job is named by the call that already read the whole ask. A second
	// round-trip for a five-token label was ~0.6 s of dead critical path in
	// front of every job, and nothing downstream waits on the name.
	if got := script.count("title"); got != 0 {
		t.Fatalf("the naming pass ran %d times beside a compile that already named the job", got)
	}
	if !strings.Contains(stdout.String(), repairedAnswer) {
		t.Fatalf("stdout does not carry the repaired deliverable:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), firstDraftAnswer) {
		t.Fatalf("stdout shipped the rejected first draft:\n%s", stdout.String())
	}
}

// The other half of the same mechanism: a gate that fails a deliverable
// against a standard the person never set buys nothing at all.
//
// Measured on a benchmark cell, that round held the worker to a working
// decision aforge had invented for itself, bought a five-turn re-run against
// it, and handed back a worse answer than the one it rejected. The citation
// invariant already refused to let such a gap grow the graph; it now refuses
// to let it redo the work either. Nothing is hidden: the review's words ride
// the delivery and the ledger records the refusal.
func TestAnUngroundedGateFailureShipsANoteInsteadOfBuyingARound(t *testing.T) {
	script := newScriptedBrain(t)
	script.inventedGap = true
	defer script.close()

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task:      "write the release note and include the migration steps",
		timeout:   60 * time.Second,
		stdout:    &stdout,
		stderr:    &stderr,
		newClient: script.client,
	})
	// It buys nothing AND it does not settle whole. Refusing where a review got
	// its words checks nothing about the world, so the finding is still standing
	// when the run hands over — see deliveredWhole.
	var status exitStatus
	if !asExitStatus(err, &status) || status != exitPartial {
		t.Fatalf("the errand settled whole over a standing finding: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	if got := script.count("gate"); got != 1 {
		t.Fatalf("the gate ran %d times, want exactly 1 — an ungrounded fail is not re-judged", got)
	}
	if got := script.count("revision"); got != 0 {
		t.Fatalf("an ungrounded gap bought %d revision passes", got)
	}
	if got := script.count("extension"); got != 0 {
		t.Fatalf("an ungrounded gap grew the graph by %d leaves", got)
	}
	if got := script.count("replan"); got != 0 {
		t.Fatalf("an ungrounded gap reached the remainder planner %d times", got)
	}
	delivered := stdout.String()
	if !strings.Contains(delivered, firstDraftAnswer) {
		t.Fatalf("the deliverable did not ship:\n%s", delivered)
	}
	// Refused-style honesty: the gap is named in the person's reading, not
	// swallowed into a silent pass.
	if !strings.Contains(delivered, inventedGapText) {
		t.Fatalf("the delivery never said what the review raised:\n%s", delivered)
	}
}

// The private store is the default because isolation is the point, and a
// default that leaves a database behind on every invocation is a mess nobody
// asked for. --keep is the way to look at what happened.
func TestDoDeletesItsPrivateStoreUnlessKept(t *testing.T) {
	for _, keep := range []bool{false, true} {
		t.Run(fmt.Sprintf("keep=%v", keep), func(t *testing.T) {
			script := newScriptedBrain(t)
			defer script.close()
			var stdout, stderr strings.Builder
			if err := doErrand(doRequest{
				task: "write the release note and include the migration steps", keep: keep,
				timeout: 60 * time.Second, stdout: &stdout, stderr: &stderr, newClient: script.client,
			}); err != nil {
				t.Fatalf("errand: %v\n%s", err, stderr.String())
			}
			home := keptHome(stderr.String())
			if !keep {
				if home != "" {
					t.Fatalf("an ephemeral run announced a kept store: %q", stderr.String())
				}
				return
			}
			if home == "" {
				t.Fatalf("--keep never said where the store is:\n%s", stderr.String())
			}
			defer os.RemoveAll(home)
			if _, err := os.Stat(filepath.Join(home, "graph.db")); err != nil {
				t.Fatalf("--keep did not keep the store: %v", err)
			}
		})
	}
}

// --json is the machine shape and it is the whole of stdout: a caller piping
// this into jq must not have to strip a footer off the front of it.
func TestDoJSONCarriesTheWholeOutcome(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task: "write the release note and include the migration steps", asJSON: true,
		timeout: 60 * time.Second, stdout: &stdout, stderr: &stderr, newClient: script.client,
	}); err != nil {
		t.Fatalf("errand: %v\n%s", err, stderr.String())
	}
	var outcome struct {
		Deliverable string   `json:"deliverable"`
		Artifacts   []string `json:"artifacts"`
		Spend       float64  `json:"spend"`
		Nodes       int      `json:"nodes"`
		Seconds     float64  `json:"seconds"`
		Settled     bool     `json:"settled"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &outcome); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, stdout.String())
	}
	if !outcome.Settled {
		t.Fatal("a run that finished reported itself unsettled")
	}
	if !strings.Contains(outcome.Deliverable, repairedAnswer) {
		t.Fatalf("json deliverable is not the repaired one: %q", outcome.Deliverable)
	}
	if outcome.Nodes < 2 {
		t.Fatalf("json node count = %d, want the original job and its extension", outcome.Nodes)
	}
	if outcome.Seconds <= 0 {
		t.Fatal("json reported no elapsed time")
	}
}

// A wall that arrives first is not a failure and not a success: what exists is
// printed, and the exit code says it is a partial.
func TestDoTimesOutWithAPartialAndCodeTwo(t *testing.T) {
	script := newScriptedBrain(t)
	script.stall = true
	defer script.close()
	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write the release note and include the migration steps",
		// Long enough for the compile to land and the leaf to start, short
		// enough that the leaf is still in the model call when the wall comes.
		timeout: 2 * time.Second, stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	var status exitStatus
	if !asExitStatus(err, &status) || status != exitPartial {
		t.Fatalf("timeout exit = %v, want exit status 2", err)
	}
	if strings.TrimSpace(stdout.String()) == "" {
		t.Fatal("a timeout printed nothing at all")
	}
}

// The price is quoted before the money moves, and a desk with nobody standing
// at it may not answer for the person. Without --yes-spend the run stops and
// says what it would have cost.
func TestDoRefusesToBuyAPlanOverTheConsentThreshold(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	// A cent of consent threshold and a measured journal cost puts every plan
	// over the line, which is the condition under test.
	t.Setenv("AFORGE_PLAN_CONSENT", "0.01")
	script.leafCost = 1.0

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task:    "write the release note and include the migration steps",
		timeout: 20 * time.Second, stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	var status exitStatus
	if !asExitStatus(err, &status) || status != exitFailed {
		t.Fatalf("refused spend exit = %v, want exit status 1", err)
	}
	if !strings.Contains(stderr.String(), "--yes-spend") {
		t.Fatalf("the refusal never named the way to approve it:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "$") {
		t.Fatalf("the refusal never quoted a price:\n%s", stderr.String())
	}
}

// -w is where the files go, and it has to survive the store: the whole reason
// to name a directory is that the private database is about to be deleted.
func TestDoLeavesArtifactsUnderTheNamedWorkspace(t *testing.T) {
	script := newScriptedBrain(t)
	script.writeFile = true
	defer script.close()
	workspace := t.TempDir()
	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task: "write the release note and include the migration steps", workspace: workspace,
		timeout: 60 * time.Second, stdout: &stdout, stderr: &stderr, newClient: script.client,
	}); err != nil {
		t.Fatalf("errand: %v\n%s", err, stderr.String())
	}
	var found string
	_ = filepath.Walk(workspace, func(path string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() && filepath.Base(path) == "notes.md" {
			found = path
		}
		return nil
	})
	if found == "" {
		t.Fatalf("nothing the job wrote survived under %s", workspace)
	}
	if !strings.Contains(stdout.String(), found) {
		t.Fatalf("the footer never named the file it left behind:\n%s", stdout.String())
	}
}

// The defect this fixes cost a benchmark run its whole point. Sent at a
// project with `-w`, the errand worked in a freshly created empty subdirectory
// of it: the file it was told to fix was not there to read, so it invented a
// module from nothing, tested its invention, and reported success while the
// person's file sat byte-identical beside it.
//
// The directory a person names IS the working directory. The proof is the edit
// tool, which replaces an exact string in an existing file: it can only succeed
// if the real file was visible from where the leaf ran.
func TestDoEditsTheNamedDirectoryInPlace(t *testing.T) {
	script := newScriptedBrain(t)
	script.editPath = "intervals.py"
	// The gate is not what this test is about, and a scripted gap quoting
	// another test's request is ungrounded against this one — which is now a
	// partial settlement rather than a silent exit 0. Let the delivery stand so
	// the assertion below is about the edit and nothing else.
	script.gatePasses = true
	defer script.close()

	workspace := t.TempDir()
	target := filepath.Join(workspace, script.editPath)
	if err := os.WriteFile(target, []byte(originalSource), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task: "fix the failing test in intervals.py", workspace: workspace,
		timeout: 60 * time.Second, stdout: &stdout, stderr: &stderr, newClient: script.client,
	}); err != nil {
		t.Fatalf("errand: %v\n%s", err, stderr.String())
	}
	if script.count("edited") == 0 {
		t.Fatalf("the leaf never reached the file it was sent to fix:\n%s", stderr.String())
	}

	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), fixedLine) {
		t.Fatalf("%s was not edited in place:\n%s", target, string(after))
	}
	if strings.Contains(string(after), brokenLine) {
		t.Fatalf("the broken line survived the edit:\n%s", string(after))
	}

	// Nothing of the engine's may remain in someone's project. The scratch
	// directories are the second-order half of the same defect: leftover
	// task-2/ folders broke the user's own pytest run with a duplicate module
	// basename collection error.
	entries, err := os.ReadDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("the run left %s/ inside the person's directory", entry.Name())
		}
		if entry.Name() != script.editPath {
			t.Fatalf("the run left %s beside the person's files", entry.Name())
		}
	}
}

// Saying nothing means here, which is what every other agent a person runs
// from a terminal means by it.
func TestErrandWorkspaceDefaultsToTheCurrentDirectory(t *testing.T) {
	here, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	got, err := errandWorkspace("")
	if err != nil {
		t.Fatal(err)
	}
	if got != here {
		t.Fatalf("default workspace = %q, want the process directory %q", got, here)
	}
	// A named one is resolved against the same place rather than left relative,
	// because the leaf that will use it does not run from here.
	named, err := errandWorkspace("sub/dir")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(here, "sub", "dir"); named != want {
		t.Fatalf("named workspace = %q, want %q", named, want)
	}
}

// stderr is the only window a person has into a headless run, and it was
// shut. Every line it printed hung off a node changing status, a node's first
// status is Pending, and Pending is skipped — so a run whose leaf was never
// claimed printed nothing at all for the whole of its life and then exited 2.
// The ask becoming work is said out loud now, before any leaf moves.
func TestDoReportsItsProgressOnStderr(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task: "write the release note and include the migration steps", workspace: t.TempDir(),
		timeout: 60 * time.Second, stdout: &stdout, stderr: &stderr, newClient: script.client,
	}); err != nil {
		t.Fatalf("errand: %v\n%s", err, stderr.String())
	}
	said := stderr.String()
	understood := strings.Index(said, "understood")
	if understood < 0 {
		t.Fatalf("stderr never said the ask became work:\n%s", said)
	}
	landed := strings.Index(said, statusMark(store.Done))
	if landed < 0 {
		t.Fatalf("stderr never reported a node landing:\n%s", said)
	}
	if understood > landed {
		t.Fatalf("the structure was announced after the work finished:\n%s", said)
	}
}

// A run that has stopped producing evidence has to account for itself. This is
// the trace that prompted it: fifteen minutes of a completely empty terminal
// behind a leaf that was never claimed, then exit 2. One structural read — no
// model call, no flag — turns an invisible hang into a diagnosable one.
func TestDoSaysWhatItIsWaitingOnWhenNothingMoves(t *testing.T) {
	root := t.TempDir()
	graph, err := store.Open(filepath.Join(root, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	session := "headless-quiet"
	command, err := graph.RequestCommand(store.Command{
		SessionID: session, Kind: store.CommandSplice, Instruction: "fix the failing test",
	})
	if err != nil {
		t.Fatal(err)
	}
	// One task, admitted to this errand and never claimed by anyone — the
	// shape of the wedged run exactly.
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-1", Brief: "fix the failing test", Stage: 0},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: session, Intent: "fix the failing test"}); err != nil {
		t.Fatal(err)
	}

	var progress strings.Builder
	watcher := &settlementWatch{
		graph: graph, session: session, commandSeq: command.Seq,
		refused: make(chan planEstimate, 1), progress: &progress,
		started: time.Now(), quiet: 50 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := watcher.wait(ctx); err != nil {
		t.Fatal(err)
	}
	said := progress.String()
	for _, phrase := range []string{"still waiting", "1 task pending", "none running"} {
		if !strings.Contains(said, phrase) {
			t.Fatalf("the quiet line never said %q:\n%s", phrase, said)
		}
	}
}

// THE WAITING LINE'S CLOCK ONLY EVER GOES FORWARD.
//
// Observed on a live run: `30s`, `30s`, `1m0s`, `30s`. The line was printing the
// SILENCE — how long since the journal last moved — so every leaf that made any
// progress at all reset it, and a person reading four lines in a row saw a
// stopwatch somebody kept restarting. A number that goes backwards is not a
// duration, it is a puzzle. What the line is for is "how long has this been
// going on", which is the run's own clock and the one that cannot run backwards.
func TestTheWaitingLinesElapsedNeverGoesBackwards(t *testing.T) {
	root := t.TempDir()
	graph, err := store.Open(filepath.Join(root, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	session := "headless-monotonic"
	command, err := graph.RequestCommand(store.Command{
		SessionID: session, Kind: store.CommandSplice, Instruction: "fix the failing test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-1", Brief: "fix the failing test", Stage: 0},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: session, Intent: "fix the failing test"}); err != nil {
		t.Fatal(err)
	}

	var progress strings.Builder
	watcher := &settlementWatch{
		graph: graph, session: session, commandSeq: command.Seq,
		refused: make(chan planEstimate, 1), progress: &progress,
		started: time.Now().Add(-2 * time.Minute), quiet: time.Millisecond,
	}
	// Four lines with the journal moving between every one of them — which is
	// exactly the run that produced the reported sequence.
	for range 4 {
		watcher.lastMoved, watcher.lastSaid = time.Now(), time.Time{}
		time.Sleep(3 * time.Millisecond)
		if err := watcher.saySomethingIfQuiet(); err != nil {
			t.Fatal(err)
		}
	}
	said := strings.TrimSpace(progress.String())
	lines := strings.Split(said, "\n")
	if len(lines) != 4 {
		t.Fatalf("the watcher said %d lines, want four:\n%s", len(lines), said)
	}
	previous := time.Duration(0)
	for _, line := range lines {
		_, tail, found := strings.Cut(line, "\u2014 ")
		if !found {
			t.Fatalf("the line carries no elapsed: %q", line)
		}
		elapsed, err := time.ParseDuration(strings.TrimSpace(tail))
		if err != nil {
			t.Fatalf("the elapsed is not a duration (%q): %v", tail, err)
		}
		if elapsed < 2*time.Minute {
			t.Fatalf("the line reports the silence rather than the run: %q", line)
		}
		if elapsed < previous {
			t.Fatalf("the clock went backwards, %s after %s:\n%s", elapsed, previous, said)
		}
		previous = elapsed
	}
}

// A JOURNAL THAT IS MOVING IS NOT A JOB THAT IS MOVING.
//
// The quiet line used to be the else-branch of the watermark: any event at all
// reset the clock, and on a store where something else is journaling — a
// sibling errand billing usage rows, a resident writing its own history — the
// accounting was never reached at all. The run that reported this was wedged on
// one node for its whole life while the journal grew steadily beside it, and it
// printed nothing. So the clock belongs to this errand's own nodes, and events
// that are nobody's business here may not silence it.
func TestTheWaitingLineSurvivesAJournalThatIsMovingElsewhere(t *testing.T) {
	root := t.TempDir()
	graph, err := store.Open(filepath.Join(root, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	session := "headless-noisy"
	command, err := graph.RequestCommand(store.Command{
		SessionID: session, Kind: store.CommandSplice, Instruction: "fix the failing test",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Two tasks: the one that is wedged, and the sibling whose chatter used to
	// silence the watcher on its behalf.
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-1", Brief: "fix the failing test", Stage: 0},
		{ID: "task-2", Parent: "task-1", Brief: "keep the fixtures current", Stage: 0},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: session, Intent: "fix the failing test"}); err != nil {
		t.Fatal(err)
	}

	var progress strings.Builder
	watcher := &settlementWatch{
		graph: graph, session: session, commandSeq: command.Seq,
		refused: make(chan planEstimate, 1), progress: &progress,
		started: time.Now(), quiet: 20 * time.Millisecond,
	}
	// The journal grows the whole time the watcher is waiting, and not one row
	// of it is a node changing state.
	noise, stop := context.WithCancel(context.Background())
	defer stop()
	go func() {
		for noise.Err() == nil {
			_ = graph.RecordUsage(store.NodeUsage{
				NodeID: "task-2", PromptTokens: 10, CompletionTokens: 1,
				Cost: 0.0001, Model: "z-ai/glm-5.3-flash",
			})
			time.Sleep(5 * time.Millisecond)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	if _, err := watcher.wait(ctx); err != nil {
		t.Fatal(err)
	}
	stop()
	if said := progress.String(); !strings.Contains(said, "still waiting") {
		t.Fatalf("the journal moved beside a wedged node and the watcher said nothing:\n%s", said)
	}
}

// The line that says nothing useful is barely better than no line. A run that
// spent fifteen minutes inside one model call reported "1 task pending, 1
// running" and then exited 2, and neither the model nor when it had last been
// heard from appeared anywhere. Both are already in the journal: the usage row
// carries the name of the model that served the call.
//
// The other half is the emptiness law. Before any call has been recorded there
// is nothing to say about calls, and the line says nothing — never "none",
// never "0s ago".
func TestTheWaitingLineNamesTheLastModelCall(t *testing.T) {
	root := t.TempDir()
	graph, err := store.Open(filepath.Join(root, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	session := "headless-lastcall"
	command, err := graph.RequestCommand(store.Command{
		SessionID: session, Kind: store.CommandSplice, Instruction: "fix the failing test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-1", Brief: "fix the failing test", Stage: 0},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: session, Intent: "fix the failing test"}); err != nil {
		t.Fatal(err)
	}

	var progress strings.Builder
	watcher := &settlementWatch{
		graph: graph, session: session, commandSeq: command.Seq,
		refused: make(chan planEstimate, 1), progress: &progress,
		started: time.Now(), quiet: time.Millisecond,
	}
	watcher.lastMoved, watcher.lastSaid = time.Time{}, time.Time{}
	if err := watcher.saySomethingIfQuiet(); err != nil {
		t.Fatal(err)
	}
	beforeAnyCall := progress.String()
	if !strings.Contains(beforeAnyCall, "still waiting") {
		t.Fatalf("the watcher said nothing at all:\n%s", beforeAnyCall)
	}
	if strings.Contains(beforeAnyCall, "last call") {
		t.Fatalf("a run with no call recorded invented one: %q", beforeAnyCall)
	}

	if err := graph.RecordUsage(store.NodeUsage{
		NodeID: "task-1", PromptTokens: 900, CompletionTokens: 40,
		Cost: 0.0021, Model: "z-ai/glm-5.3-flash",
	}); err != nil {
		t.Fatal(err)
	}
	progress.Reset()
	watcher.lastSaid = time.Time{}
	if err := watcher.saySomethingIfQuiet(); err != nil {
		t.Fatal(err)
	}
	said := progress.String()
	for _, phrase := range []string{"last call", "z-ai/glm-5.3-flash", " ago"} {
		if !strings.Contains(said, phrase) {
			t.Fatalf("the waiting line never said %q:\n%s", phrase, said)
		}
	}
	// The run's own clock still closes the line, and it is still the run's.
	if !strings.Contains(said, "\u2014 ") {
		t.Fatalf("the elapsed left the line: %q", said)
	}

	// A call the process heard back from a moment ago outranks the journal's
	// row: a bare leaf books its usage only when it finishes, and the line is
	// for the ten minutes before that.
	calllog.Append(calllog.Record{
		Time:  time.Now().Format("2006-01-02T15:04:05.000Z07:00"),
		Model: "nvidia/nemotron-3.5-lightning", Tag: "leaf", Node: "task-1", Status: 200,
	})
	progress.Reset()
	watcher.lastSaid = time.Time{}
	if err := watcher.saySomethingIfQuiet(); err != nil {
		t.Fatal(err)
	}
	if said := progress.String(); !strings.Contains(said, "last call nvidia/nemotron-3.5-lightning 0s ago") {
		t.Fatalf("the line did not read the call the process just heard:\n%s", said)
	}

	// A call heard before this errand started is another errand's, and the
	// journal's row — which is scoped to this one — is what the line reads.
	watcher.started = time.Now().Add(time.Minute)
	progress.Reset()
	watcher.lastSaid = time.Time{}
	if err := watcher.saySomethingIfQuiet(); err != nil {
		t.Fatal(err)
	}
	if said := progress.String(); !strings.Contains(said, "z-ai/glm-5.3-flash") {
		t.Fatalf("a call from before the errand outranked the journal:\n%s", said)
	}
}

// --JSON IS ONE OBJECT ON STDOUT, ON EVERY PATH INCLUDING THE ONES THAT FAILED.
//
// A store that will not open, a directory that cannot be made, a resident that
// never picks the command up: each of those returned an error that main printed
// as "error: ..." on stderr with nothing whatsoever on stdout — which is exactly
// what a crashed process looks like to the thing reading the pipe. A caller
// could not tell a bad path from a segfault. So the failure is an outcome like
// any other: the object is there, the sentence is in its error field, and the
// exit code is 1, the same 1 the table has always promised for a run that
// produced nothing usable.
func TestJSONPrintsAnObjectWhenTheErrandCannotEvenStart(t *testing.T) {
	// A regular file where the store's directory would have to be. Nothing
	// about this run reaches a model, a lease or a journal.
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("in the way"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write the release note", database: filepath.Join(blocked, "graph.db"),
		asJSON: true, timeout: 10 * time.Second,
		stdout: &stdout, stderr: &stderr,
	})
	var status exitStatus
	if !asExitStatus(err, &status) || status != exitFailed {
		t.Fatalf("a run that could not start exited %v, want exit status 1", err)
	}
	outcome := decodeErrand(t, stdout.String())
	if strings.TrimSpace(outcome.Error) == "" {
		t.Fatalf("the object carries no error: %s", stdout.String())
	}
	if !strings.Contains(outcome.Error, "store directory") {
		t.Fatalf("the error does not say what went wrong: %q", outcome.Error)
	}
	if outcome.Settled {
		t.Fatal("a run that never started reported itself settled")
	}
	if strings.TrimSpace(outcome.Deliverable) != "" {
		t.Fatalf("a failure was dressed up as an answer: %q", outcome.Deliverable)
	}
}

// The same failure without --json is the same failure it always was: a sentence
// on stderr from main, and no half-JSON on the stream a person is reading.
func TestAFailedErrandWithoutJSONStillJustReturnsTheError(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("in the way"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write the release note", database: filepath.Join(blocked, "graph.db"),
		timeout: 10 * time.Second, stdout: &stdout, stderr: &stderr,
	})
	if err == nil || !strings.Contains(err.Error(), "store directory") {
		t.Fatalf("the error did not reach main: %v", err)
	}
	var status exitStatus
	if asExitStatus(err, &status) {
		t.Fatalf("a plain run invented an exit code for a failure: %v", err)
	}
	if strings.TrimSpace(stdout.String()) != "" {
		t.Fatalf("stdout carried something on a run that failed: %q", stdout.String())
	}
}

// AFORGE_HOME moves the whole home in one word — the seam a harness runs a
// fleet of isolated aforges through.
func TestAforgeHomeMovesTheDefaultStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv(homepkg.EnvVar, home)
	if got, want := defaultChatDB(), filepath.Join(home, "graph.db"); got != want {
		t.Fatalf("default store = %q, want %q", got, want)
	}
}

// The defect this fixes cost a benchmark two whole cells. "Reconcile
// bank_export.csv against ledger.csv for June 2026 and flag every discrepancy"
// is a plain one-shot analytical ask, and the words "every discrepancy" tripped
// the temporal recognizer's `every <word>` cue. The compiler drafted a STANDING
// RULE for it, invented a two-minute cadence and a daily budget out of nothing,
// and put a ratification card to a process with nobody at the keyboard. The run
// exited 1 in three seconds having done zero work, with the interactive question
// sitting in the JSON `deliverable` field where a caller reads the answer.
//
// `aforge do` IS the answer to that question. A person who typed the verb has
// already chosen "once, not standing", so the classification is settled by the
// surface before a model reads a word: the temporal route is never taken, and
// the ask compiles, plans, runs and delivers exactly like any other errand.
func TestDoRunsAStandingSoundingAskOnceInsteadOfAskingToRatifyIt(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	script.gatePasses = true

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task:    "Reconcile bank_export.csv against ledger.csv for June 2026 and flag every discrepancy",
		asJSON:  true,
		timeout: 60 * time.Second, workspace: t.TempDir(),
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	if err != nil {
		t.Fatalf("a plain one-shot ask did not run: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
	}
	// The structural pin: the temporal compiler was never reached at all. Its
	// prompt is the only door to a charter draft on this path, and a headless
	// errand does not have that door.
	if got := script.count("standing"); got != 0 {
		t.Fatalf("the temporal compiler ran %d times on a one-shot errand, want 0", got)
	}
	if got := script.count("compile"); got == 0 {
		t.Fatal("the ordinary intent compiler never saw the ask")
	}
	outcome := decodeErrand(t, stdout.String())
	if !outcome.Settled || outcome.BlockedOn != "" {
		t.Fatalf("the errand did not settle cleanly: %+v", outcome)
	}
	// The work actually happened, and what the caller reads is its product.
	if script.count("draft") == 0 {
		t.Fatalf("no leaf ever ran:\n%s", stderr.String())
	}
	if !strings.Contains(outcome.Deliverable, firstDraftAnswer) {
		t.Fatalf("the deliverable is not the work product: %q", outcome.Deliverable)
	}
	assertErrandIsHonest(t, outcome, nil)
}

// Defense in depth for the same law. The surface fact settles the classification
// in the compiler, but a charter draft can still arrive from a provider that
// emitted the key uninvited or a compiler that is not the head's. A draft that
// reaches a process with no keyboard is auto-resolved the way the caller already
// chose — once, not standing — journaled as retired, and then the work RUNS.
// The failure mode this replaces is the one that matters: exiting having done
// nothing.
func TestDoResolvesAnUnexpectedCharterDraftAsOnceAndRunsTheWork(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	script.compileDraftsCharter = true
	script.gatePasses = true

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task:    "Reconcile bank_export.csv against ledger.csv for June 2026 and flag every discrepancy",
		asJSON:  true,
		timeout: 60 * time.Second, workspace: t.TempDir(),
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	if err != nil {
		t.Fatalf("a charter draft ended the errand instead of being resolved: %v\nstderr:\n%s",
			err, stderr.String())
	}
	outcome := decodeErrand(t, stdout.String())
	if outcome.BlockedOn != "" {
		t.Fatalf("the ratification card reached the caller anyway: %q", outcome.BlockedOn)
	}
	if script.count("draft") == 0 {
		t.Fatalf("the draft was resolved and the work still never ran:\n%s", stderr.String())
	}
	if !strings.Contains(outcome.Deliverable, firstDraftAnswer) {
		t.Fatalf("the deliverable is not the work product: %q", outcome.Deliverable)
	}
	assertErrandIsHonest(t, outcome, nil)
}

// A compiler askback is a dead end on a surface with no keyboard, and stopping
// on one is not the careful answer — it is a run that compiled, asked into an
// empty room, and handed its caller an interactive card where work was
// expected. So the errand takes the answer the compiler itself ranked first,
// declares it, and does the job.
//
// This is the same choice the verb already makes about standing-or-once, one
// rung further in: everything `do` can decide for itself, it decides, and says
// that it did.
func TestDoAssumesTheCompilerQuestionNobodyIsHereToAnswerAndRunsTheWork(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	script.compilerAsks = unanswerableQuestion
	script.gatePasses = true

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task:    "reconcile the two ledgers and tell me what is wrong",
		asJSON:  true,
		timeout: 60 * time.Second, workspace: t.TempDir(),
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	if err != nil {
		t.Fatalf("a question the errand could have assumed ended the run: %v\nstderr:\n%s", err, stderr.String())
	}
	outcome := decodeErrand(t, stdout.String())
	if outcome.BlockedOn != "" {
		t.Fatalf("the question reached the caller anyway: %q", outcome.BlockedOn)
	}
	if !strings.Contains(outcome.Deliverable, firstDraftAnswer) {
		t.Fatalf("the deliverable is not the work product: %q", outcome.Deliverable)
	}
	assertErrandIsHonest(t, outcome, err)
}

// THE PERSON'S OWN WORDS ARE ALWAYS A VALID GOAL. A compile that decoded with
// no goal in it used to end the run at the first call — 229 s and $0.021 for
// zero nodes on a 416-word brief — while the instruction sat in the request the
// whole time. Now the request stands as the goal, the work runs, and the one
// line of news is said on stderr where this surface narrates: a substitution
// nobody can see is the defect #311 and #314 closed (#335).
func TestDoRunsTheWorkWhenTheCompileSuppliesNoGoalAndSaysSo(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	script.compileBlankGoal = true
	script.gatePasses = true

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task:    "write the release note and include the migration steps",
		asJSON:  true,
		timeout: 60 * time.Second, workspace: t.TempDir(),
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	if err != nil {
		t.Fatalf("a blank goal ended the run: %v\nstderr:\n%s", err, stderr.String())
	}
	outcome := decodeErrand(t, stdout.String())
	if strings.Contains(outcome.Deliverable, "couldn't apply") || !strings.Contains(outcome.Deliverable, firstDraftAnswer) {
		t.Fatalf("the deliverable is not the work product: %q", outcome.Deliverable)
	}
	if !strings.Contains(stderr.String(), "your request stands as the goal, word for word") {
		t.Fatalf("stderr never said the compiler supplied no reading:\n%s", stderr.String())
	}
	assertErrandIsHonest(t, outcome, err)
}

// ONE LEDGER, ONE NUMBER. The acceptance pass — the plan model reading the
// request for the behaviours it states — ran on the plan client's snapshot,
// which carries no usage journal, and its usage was thrown away; a run's
// printed total was short by exactly that call (#380). Every call the run
// makes before its job exists is billed to the spine, so the spine's rows
// number the compile and the acceptance pass together.
func TestTheAcceptancePassIsBilledToTheSpine(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	script.gatePasses = true
	script.acceptancePoints = `{"points":[{"text":"the count is written to count.txt"}]}`

	database := filepath.Join(t.TempDir(), "graph.db")
	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task:     "count the lines in notes.txt and write the count to count.txt",
		database: database, asJSON: true,
		timeout: 60 * time.Second, workspace: t.TempDir(),
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	if err != nil {
		t.Fatalf("the errand did not settle: %v\nstderr:\n%s", err, stderr.String())
	}
	if script.count("acceptance") != 1 {
		t.Fatalf("the acceptance pass ran %d times, want 1", script.count("acceptance"))
	}
	graph, err := store.Open(database)
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	spend, err := graph.SpendSinceSeq("", 0)
	if err != nil {
		t.Fatal(err)
	}
	// Every other scripted call completes ten tokens, so the acceptance pass's
	// own figure is visible in the spine's sum exactly once.
	rest := spend.Spine.CompletionTokens - acceptanceCompletionTokens
	if rest < 0 || rest%10 != 0 {
		t.Fatalf("the spine's completion tokens are %d; the acceptance pass's %d are not among them",
			spend.Spine.CompletionTokens, acceptanceCompletionTokens)
	}
}

// The other half of the contract, which the law above narrows but does not
// repeal: a question that does reach the end of a headless run must not end it
// in silence. Three seconds, five thousandths of a cent and an empty stdout is
// indistinguishable from a fast cheap success in a pipeline, which is exactly
// how the original defect went unnoticed. So it is loud — the question verbatim
// on stderr and a line saying nobody here could answer it — while stdout, which
// is the only stream a pipeline reads, stays empty of it.
func TestABlockedErrandSaysSoOnStderrAndNowhereElse(t *testing.T) {
	var stdout, stderr strings.Builder
	outcome := headlessOutcome{BlockedOn: unanswerableQuestion, status: exitFailed}
	err := reportErrand(doRequest{asJSON: true, stdout: &stdout, stderr: &stderr}, outcome)

	var status exitStatus
	if !asExitStatus(err, &status) || status != exitFailed {
		t.Fatalf("a blocked errand exited %v, want exit status 1", err)
	}
	said := stderr.String()
	if !strings.Contains(said, unanswerableQuestion) {
		t.Fatalf("stderr never carried the question:\n%s", said)
	}
	if !strings.Contains(said, "headless mode cannot answer") {
		t.Fatalf("stderr never said why nothing was done:\n%s", said)
	}
	decoded := decodeErrand(t, stdout.String())
	if !strings.Contains(decoded.BlockedOn, unanswerableQuestion) {
		t.Fatalf("blocked_on does not carry the question: %+v", decoded)
	}
	if strings.TrimSpace(decoded.Deliverable) != "" {
		t.Fatalf("the question polluted the deliverable: %q", decoded.Deliverable)
	}
	assertErrandIsHonest(t, decoded, err)
}

// The pin. `settled` says the errand is over, the exit code says whether it
// worked, and blocked_on says a question stopped it — three fields that a caller
// reads together and that may never contradict each other. The rule that was
// broken and is now enforced everywhere: a question is never a deliverable, and
// a run that produced one is never reported as having produced work.
func TestErrandOutcomesNeverContradictThemselves(t *testing.T) {
	root := t.TempDir()
	graph, err := store.Open(filepath.Join(root, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	for _, shape := range []struct {
		name     string
		question string
		rejected string
		landed   string
	}{
		{name: "work landed", landed: "the reconciliation found three breaks"},
		{name: "stopped on a question", question: "Which ledger is authoritative?", rejected: "asked the user"},
		{name: "refused without a question", rejected: "splice failed: the planner is unreachable"},
	} {
		t.Run(shape.name, func(t *testing.T) {
			session := "headless-" + strings.ReplaceAll(shape.name, " ", "-")
			command, err := graph.RequestCommand(store.Command{
				SessionID: session, Kind: store.CommandSplice, Instruction: "reconcile the ledgers",
			})
			if err != nil {
				t.Fatal(err)
			}
			if shape.question != "" {
				if _, err := graph.AskQuestion(store.AgentQuestion{
					SessionID: session, Text: shape.question, OriginCommandSeq: command.Seq,
					Urgency: store.QuestionBlocking,
				}); err != nil {
					t.Fatal(err)
				}
			}
			if shape.rejected != "" {
				if err := graph.ResolveCommand(command.Seq, store.CommandRejected, shape.rejected); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := graph.ResolveCommand(command.Seq, store.CommandApplied, "spliced 1 node"); err != nil {
					t.Fatal(err)
				}
				if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
					{ID: "task-" + session, Brief: "reconcile the ledgers", Stage: 0},
				}}, store.Provenance{Origin: store.OriginUser, SessionID: session,
					Intent: "reconcile the ledgers"}); err != nil {
					t.Fatal(err)
				}
				claim, claimed, err := graph.Claim("task-"+session, "test")
				if err != nil || !claimed {
					t.Fatalf("claim: %v (claimed=%v)", err, claimed)
				}
				if err := graph.Start(claim); err != nil {
					t.Fatal(err)
				}
				if err := graph.Complete(claim, shape.landed); err != nil {
					t.Fatal(err)
				}
			}

			watcher := &settlementWatch{
				graph: graph, session: session, commandSeq: command.Seq,
				refused: make(chan planEstimate, 1), started: time.Now(),
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			outcome, err := watcher.wait(ctx)
			if err != nil {
				t.Fatal(err)
			}
			assertErrandIsHonest(t, outcome, errandStatus(outcome))

			switch {
			case shape.question != "":
				if !strings.Contains(outcome.BlockedOn, shape.question) {
					t.Fatalf("the question never reached blocked_on: %+v", outcome)
				}
			case shape.rejected != "":
				if outcome.BlockedOn != "" {
					t.Fatalf("a plain refusal was reported as a question: %+v", outcome)
				}
				if strings.TrimSpace(outcome.Deliverable) == "" {
					t.Fatal("a plain refusal said nothing at all")
				}
			default:
				if !strings.Contains(outcome.Deliverable, shape.landed) {
					t.Fatalf("the landed work is not the deliverable: %+v", outcome)
				}
			}
		})
	}
}

// assertErrandIsHonest is the invariant every one of the paths above is held to.
func assertErrandIsHonest(t *testing.T, outcome headlessOutcome, exit error) {
	t.Helper()
	var status exitStatus
	if exit != nil && !asExitStatus(exit, &status) {
		t.Fatalf("the errand left with something that is not an exit status: %v", exit)
	}
	if strings.TrimSpace(outcome.BlockedOn) != "" {
		if status == 0 {
			t.Fatalf("a run stopped by a question reported success: %+v", outcome)
		}
		if strings.TrimSpace(outcome.Deliverable) != "" {
			t.Fatalf("a question and a deliverable were reported together: %+v", outcome)
		}
	}
	if status == 0 && !outcome.Settled {
		t.Fatalf("a run that exited zero called itself unsettled: %+v", outcome)
	}
	if status == 0 && strings.TrimSpace(outcome.BlockedOn) != "" {
		t.Fatalf("a successful run carried an unanswered question: %+v", outcome)
	}
}

// decodeErrand reads the machine shape the way a caller does.
func decodeErrand(t *testing.T, stdout string) headlessOutcome {
	t.Helper()
	var outcome headlessOutcome
	if err := json.Unmarshal([]byte(stdout), &outcome); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, stdout)
	}
	return outcome
}

// ---------------------------------------------------------------------------
// The scripted brain: one HTTP endpoint standing in for every model call the
// run makes, dispatching on the prompt that arrived. It is deliberately not a
// stub of aforge's own seams — the real compiler, planner, executor, gate and
// replan all run, and this only decides what the model says back to them.

const (
	firstDraftAnswer = "RELEASE NOTE DRAFT: the parser is faster."
	repairedAnswer   = "RELEASE NOTE FINAL: the parser is faster, and here are the migration steps."
	// citedGap quotes the user's own words, which is the one thing that lets a
	// gap commission new work. A gap that invented a requirement would be
	// refused before a planning call was made.
	citedQuote = "include the migration steps"
	// inventedQuote is the opposite: words the compiler put in its own goal
	// and the person never typed. A gap that can only quote this is aforge
	// holding aforge to a standard it wrote after reading its own output.
	inventedQuote   = "the note is addressed to an operator audience"
	inventedGapText = "the note does not address an operator audience"
	// gateCritique is the reviewer's own prose. It is journaled and readable on
	// request; it is not something the person should ever find in the answer.
	gateCritique = "the migration steps are missing"
	// artifactName is what the worker writes when the test asks it to leave
	// something on disk.
	artifactName = "notes.md"
	// The in-place edit: a file that already exists in the person's directory,
	// with one line the worker is scripted to replace. The edit tool requires
	// the old text to be found, so a successful edit is proof the real file was
	// where the leaf was standing.
	brokenLine     = "return start <= other.end and other.start < end"
	fixedLine      = "return start <= other.end and other.start <= end"
	originalSource = "def overlaps(start, end, other):\n    " + brokenLine + "\n"
	// unanswerableQuestion is a gap no errand semantics can close: not the
	// standing-or-once classification the verb already answers, and not a price
	// --yes-spend covers. Nobody is here to answer it, so the errand assumes an
	// answer and declares it — and a question that survives to the end of a run
	// anyway is said out loud rather than swallowed.
	unanswerableQuestion = "Which ledger is authoritative when the two disagree?"
)

// scriptedCharter is what a temporal compiler answers with — and what the
// benchmark's two failing cells were handed for asks that were nothing of the
// kind. The two-minute cadence is not invented here for colour; it is the
// literal default standingWatch supplies when nobody stated a rhythm.
const scriptedCharter = `{"invariant":"Reconcile bank_export.csv against ledger.csv for June 2026 and flag every discrepancy",` +
	`"watch":{"kind":"poll","cadence":"about every 2 minutes","schedule":""},` +
	`"sentinel":"Decide whether the ledgers have diverged.",` +
	`"action":"Reconcile the two ledgers and flag every discrepancy.",` +
	`"rails":{"estimated_cost_usd":0.05,"max_per_day":10,"max_per_day_justification":"caps the default worst day at about $0.50","expiry":"never"}}`

type scriptedBrain struct {
	t      *testing.T
	server *httptest.Server
	dir    string

	// stall makes every leaf call hang, so a wall can be proved.
	stall bool
	// leafFails makes every leaf call fail at the provider, so a leaf that
	// cannot do the work can be followed all the way to what the run says
	// about it.
	leafFails bool
	// inventedGap makes the gate fail the deliverable against a standard
	// nobody asked for — the shape of the one measured round that made a
	// deliverable worse.
	inventedGap bool
	// writeFile makes the first leaf write a real artifact.
	writeFile bool
	// editPath names a file already in the workspace that the first leaf edits
	// in place, which is what a coding errand actually does.
	editPath string
	// namesPath makes the worker's draft mention an absolute path it did not
	// write. It is how the difference between the two ways of answering "what
	// did this run produce?" is made visible: the workspace's registry knows it
	// produced nothing of the sort, while a regex over the worker's prose sees a
	// real file on disk and credits the run with it.
	namesPath string
	// leafCost is what each call reports spending, which is what the consent
	// desk's estimate is built from.
	leafCost float64
	// compileDraftsCharter makes the ORDINARY intent compiler hand back a
	// charter, which is the shape a provider emitting an uninvited key produces
	// — the case the structural pin cannot catch and the reconciler must.
	compileDraftsCharter bool
	// compilerAsks makes the compiler stop on a question instead of compiling.
	compilerAsks string
	// compileBlankGoal makes the compiler answer a well-formed object with no
	// goal in it — the shape a continuation restarted from the field after a
	// cut left behind, which used to end the run with "empty goal" (#335).
	compileBlankGoal bool
	// gatePasses lets a deliverable through on the first look, for the runs
	// whose subject is not the gate.
	gatePasses bool
	// longAnswer, when set, is what the worker hands back instead of the short
	// draft, and the gate passes it on sight. It is how a deliverable longer
	// than any single bound on the path can be followed from the worker's
	// mouth to the person's screen.
	longAnswer string
	// runawayLeaf makes every leaf turn ask for another tool call and bill for
	// turnTokens prompt tokens, so the leaf crosses its grant and is landed by
	// the budget rather than by finishing. It is the shape the settlement's
	// exhaustion arm exists for and the only one that reaches it.
	runawayLeaf bool
	// turnTokens is what one runaway turn bills. Zero is the ordinary ten.
	turnTokens int
	// remainderDone scripts the remainder judge to say a cut leaf left nothing
	// behind — the adversarial-but-observed answer.
	remainderDone bool
	// acceptancePoints, when set, is what the acceptance pass reads out of the
	// request: the JSON body of one {"points": [...]} answer. It is how a
	// checklist reaches the gate in a scripted run — nothing else derives one.
	acceptancePoints string
	// jobCovered answers the growth governor's satisfaction question with
	// "nothing is left", which is the reading that refused a repair round over
	// a finding the world had raised.
	jobCovered bool
	// revisionCloses runs the ordinary repair to its ordinary end: the gate
	// fails the first draft on the person's own words, the one revision it buys
	// comes back with the answer, and the second reading passes. It is the
	// common case and the one the panel read as a doubted deliverable.
	revisionCloses bool

	mu     sync.Mutex
	counts map[string]int
}

func newScriptedBrain(t *testing.T) *scriptedBrain {
	t.Helper()
	script := &scriptedBrain{t: t, dir: t.TempDir(), counts: map[string]int{}}
	script.server = httptest.NewServer(http.HandlerFunc(script.serve))
	// The catalog, the media clients and anything else that reaches for an
	// endpoint find this one; none of them are what is under test, and all of
	// them degrade cleanly against a server that has no answers for them.
	t.Setenv("AFORGE_BASE_URL", script.server.URL)
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("AFORGE_PROFILE_DIR", script.dir)
	t.Setenv("AFORGE_DAILY_BUDGET", "0")
	t.Setenv("AFORGE_PRACTICE_BUDGET", "0")
	if os.Getenv("AFORGE_PLAN_CONSENT") == "" {
		t.Setenv("AFORGE_PLAN_CONSENT", "0")
	}
	return script
}

func (s *scriptedBrain) close() { s.server.Close() }

func (s *scriptedBrain) count(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[name]
}

func (s *scriptedBrain) tally(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts[name]++
	return s.counts[name]
}

// client is the injection seam buildBrain takes: every slot in the brain gets a
// one-rung panel pointed at the scripted endpoint. One rung matters — it is
// what keeps the executor from escalating a leaf to a second model and doubling
// every count this test reads.
func (s *scriptedBrain) client(settings config.Config, model string) (*liveClient, error) {
	panel, err := router.New(router.Panel{Models: []router.Spec{{Slug: model, Price: 0.01}}},
		provider.Config{APIKey: "test-key", BaseURL: s.server.URL}, s.dir)
	if err != nil {
		return nil, err
	}
	settings.Model = model
	return adoptLiveClient(settings, model, panel), nil
}

func (s *scriptedBrain) serve(writer http.ResponseWriter, request *http.Request) {
	if !strings.HasSuffix(request.URL.Path, "/chat/completions") {
		http.Error(writer, `{"error":"no"}`, http.StatusNotFound)
		return
	}
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(writer, `{"error":"unreadable"}`, http.StatusBadRequest)
		return
	}
	body := string(raw)
	if s.stall && strings.Contains(body, "You complete one piece of work, alone, using tools") {
		s.tally("stalled")
		// Wedged, but not wedged past the test: the client's own context ends
		// this the moment the wall arrives, and the handler lets go with it so
		// the server can close.
		select {
		case <-request.Context().Done():
		case <-time.After(30 * time.Second):
		}
		return
	}
	if s.leafFails && strings.Contains(body, "You complete one piece of work, alone, using tools") {
		s.tally("leaf-failed")
		http.Error(writer, `{"error":{"message":"the model refused this request","code":400}}`,
			http.StatusBadRequest)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	fmt.Fprint(writer, s.reply(body))
}

// reply is the whole script, in the order the run reaches it.
func (s *scriptedBrain) reply(body string) string {
	switch {
	case strings.Contains(body, "You compile durable intent into one inert charter draft"):
		// Reaching this at all on a headless errand is the defect. The count is
		// the assertion; the answer is what the benchmark actually received.
		s.tally("standing")
		return s.say(scriptedCharter)

	case strings.Contains(body, "You are the intent compiler"):
		s.tally("compile")
		if s.compilerAsks != "" {
			return s.say(fmt.Sprintf(`{"goal":"","scale":"task","builds_on":[],"assumptions":[],`+
				`"question":%q,"question_options":[],"trial_of":0}`, s.compilerAsks))
		}
		if s.compileBlankGoal {
			return s.say(`{"title":"Release note and migration","scale":"task",` +
				`"builds_on":[],"assumptions":[],"question":"","trial_of":0}`)
		}
		if s.compileDraftsCharter {
			return s.say(`{"goal":"","scale":"task","builds_on":[],"assumptions":[],` +
				`"question":"Stand this rule up?","question_options":[` +
				`{"label":"yes, stand this up","value":"ratify"},` +
				`{"label":"once, not standing","value":"once"}],` +
				`"trial_of":0,"charter":` + scriptedCharter + `}`)
		}
		// Task scale: one worker end to end, which is the shape that still
		// earns a written working method and still faces the gate.
		return s.say(`{"goal":"Write the release note for the parser work, including the migration steps.",` +
			`"title":"Release note and migration",` +
			`"scale":"task","builds_on":[],"assumptions":[],"question":"","trial_of":0}`)

	case strings.Contains(body, "You read one request and list the behaviours it states"):
		s.tally("acceptance")
		points := s.acceptancePoints
		if points == "" {
			points = `{"points":[]}`
		}
		// Billed with a figure no other scripted call reports, so a test can
		// see this one row in a sum (TestTheAcceptancePassIsBilledToTheSpine).
		return strings.Replace(s.say(points), `"completion_tokens":10`,
			fmt.Sprintf(`"completion_tokens":%d`, acceptanceCompletionTokens), 1)

	case strings.Contains(body, "You decide whether a job still needs work added to it"):
		s.tally("satisfied")
		if s.jobCovered {
			return s.say(`{"complete":true,"uncovered":[]}`)
		}
		return s.say(`{"complete":false,"uncovered":[]}`)

	case strings.Contains(body, "You write the working method for one agent"):
		s.tally("contract")
		return s.say(`{"contract":"Read the changelog first. Done means the note names every migration step a reader has to take."}`)

	case strings.Contains(body, "You name jobs for a narrow task list"):
		s.tally("title")
		return s.say("Release note and migration")

	case strings.Contains(body, "You break a goal into its ordered stages"),
		strings.Contains(body, "settled points"):
		// The remainder planner's opening pass. Refusing it here proves the
		// documented fallback — one fresh worker on the remainder — rather than
		// leaving the replan untested when a planner is unavailable.
		s.tally("replan")
		return s.say("no plan today")

	case strings.Contains(body, "You are the final gate"):
		round := s.tally("gate")
		// The gate's refusal shape follows what it is judging. Over a changed
		// tree a fail has to name one file of the record, so the stub reads the
		// record it was handed rather than inventing a path — which is the same
		// contract a real judge is held to, and the only way a stub can stay
		// honest to a schema that moves with the subject.
		fail := func(gaps, quote string) string {
			verdict := fmt.Sprintf(`{"pass":false,"gaps":%q,"quote":%q,"exercised":false`, gaps, quote)
			if file := gateSubjectFile(body); file != "" {
				verdict += fmt.Sprintf(`,"file":%q`, file)
			}
			return s.say(verdict + "}")
		}
		if s.longAnswer != "" {
			return s.say(`{"pass":true,"gaps":"","quote":"","exercised":true}`)
		}
		if s.inventedGap {
			// The quote is a span of the compiled goal's own working
			// decisions, not of anything the person typed.
			return fail(inventedGapText, inventedQuote)
		}
		if s.gatePasses {
			return s.say(`{"pass":true,"gaps":"","quote":"","exercised":true}`)
		}
		if s.revisionCloses {
			if round == 1 {
				return fail(gateCritique, citedQuote)
			}
			return s.say(`{"pass":true,"gaps":"","quote":"","exercised":true}`)
		}
		if round <= 2 {
			// The first draft and the revision of it are both judged short of
			// the ask, and the gap quotes the ask itself — the one thing that
			// buys another round of real work.
			return fail("the migration steps are missing", citedQuote)
		}
		return s.say(`{"pass":true,"gaps":"","quote":"","exercised":true}`)

	case strings.Contains(body, "A worker was stopped mid-assignment because it ran out of the room"):
		s.tally("remainder")
		if s.remainderDone {
			return s.say(`{"done":true}`)
		}
		return s.say(`{"done":false,"remaining":"finish the migration steps"}`)

	case strings.Contains(body, "You judge whether a finished job taught"):
		s.tally("distill")
		return s.say(`{"facts":[]}`)

	case strings.Contains(body, "You complete one piece of work, alone, using tools"):
		return s.leaf(body)
	}
	// Anything else the resident asks about itself gets a shrug it can absorb.
	s.tally("other")
	return s.say("{}")
}

// gateSubjectFile is the first file the gate's own prompt lists as part of the
// change it is judging, or empty where the run left nothing behind and the
// deliverable is the worker's message. It reads the block the gate composed
// rather than a path the test happens to know, so a stub cannot answer with a
// file the judge was never shown.
func gateSubjectFile(body string) string {
	// The stub is handed the encoded request, so the prompt's own newlines
	// arrive as the two characters JSON spells them with. Reading them back is
	// what makes this a reader of the block the gate composed rather than of
	// the transport that carried it.
	body = strings.ReplaceAll(body, `\n`, "\n")
	head := "Sources the run wrote or changed:\n"
	start := strings.Index(body, head)
	if start < 0 {
		return ""
	}
	line := body[start+len(head):]
	if end := strings.IndexByte(line, '\n'); end >= 0 {
		line = line[:end]
	}
	if open := strings.LastIndex(line, " ("); open >= 0 {
		line = line[:open]
	}
	return strings.TrimSpace(line)
}

// leaf answers as the worker. Which worker it is reads off the inputs it was
// given, which is how the product itself distinguishes the three: a first
// draft, the revision the gate's critique bought, and the work the cited gap
// commissioned.
func (s *scriptedBrain) leaf(body string) string {
	switch {
	case s.runawayLeaf:
		// It never says it is finished, so what stops it is its own envelope.
		turn := s.tally("runaway")
		return s.tool("write", fmt.Sprintf(`{"path":"scratch-%d.txt","text":"still working"}`, turn))
	case s.longAnswer != "":
		s.tally("draft")
		return s.say(s.longAnswer)
	case strings.Contains(body, "Finish work a previous agent started"):
		s.tally("extension")
		return s.say(repairedAnswer)
	case strings.Contains(body, "A reviewer compared the previous attempt"):
		s.tally("revision")
		if s.revisionCloses {
			return s.say(repairedAnswer)
		}
		return s.say(firstDraftAnswer + " (revised, still nothing about migrating)")
	// The first leaf turn edits; the task itself names the file, so the guard
	// counts turns rather than looking for the path in the transcript.
	case s.editPath != "" && s.count("edited") == 0:
		s.tally("edited")
		return s.tool("edit", fmt.Sprintf(`{"path":%q,"old":%q,"new":%q}`, s.editPath, brokenLine, fixedLine))
	case s.writeFile && !strings.Contains(body, artifactName):
		// The honest way to leave a file behind is the tool the product gives
		// the worker for it, so the artifact reaches the outcome the way every
		// real artifact does rather than by being asserted into existence.
		s.tally("wrote")
		return s.tool("write", fmt.Sprintf(`{"path":%q,"text":"migration steps go here"}`, artifactName))
	default:
		s.tally("draft")
		if s.namesPath != "" {
			return s.say(firstDraftAnswer + " I read " + s.namesPath + " to write it.")
		}
		return s.say(firstDraftAnswer)
	}
}

// acceptanceCompletionTokens is what the scripted acceptance pass reports
// completing: a figure that is not ten, so its row is visible in a total.
const acceptanceCompletionTokens = 777

func (s *scriptedBrain) say(content string) string {
	encoded, _ := json.Marshal(content)
	prompt := s.billedTokens()
	return fmt.Sprintf(`{"model":"scripted","choices":[{"index":0,"finish_reason":"stop",`+
		`"message":{"role":"assistant","content":%s}}],`+
		`"usage":{"prompt_tokens":%d,"completion_tokens":10,"total_tokens":%d,"cost":%f}}`,
		string(encoded), prompt, prompt+10, s.leafCost)
}

func (s *scriptedBrain) tool(name, arguments string) string {
	encoded, _ := json.Marshal(arguments)
	prompt := s.billedTokens()
	return fmt.Sprintf(`{"model":"scripted","choices":[{"index":0,"finish_reason":"tool_calls",`+
		`"message":{"role":"assistant","content":"","tool_calls":[{"id":"call-1","type":"function",`+
		`"function":{"name":%q,"arguments":%s}}]}}],`+
		`"usage":{"prompt_tokens":%d,"completion_tokens":10,"total_tokens":%d,"cost":%f}}`,
		name, string(encoded), prompt, prompt+10, s.leafCost)
}

// billedTokens is what one scripted call reports spending. Ten is the ordinary
// figure every test that is not about budgets reads; a run that has to cross a
// leaf's grant says how big its turns are instead of taking seven thousand of
// them to get there.
func (s *scriptedBrain) billedTokens() int {
	if s.turnTokens > 0 {
		return s.turnTokens
	}
	return 10
}

func asExitStatus(err error, status *exitStatus) bool {
	coded, ok := err.(exitStatus)
	if !ok {
		return false
	}
	*status = coded
	return true
}

func keptHome(stderr string) string {
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, "record kept at ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "record kept at "))
		}
	}
	return ""
}

// The narration may not contradict the artifacts.
//
// Measured: a run returned rc=0 with five correct deliverable files on disk and
// the closing text "The work was still mid-flight when time ran out … Nothing
// here is the answer." Two more shapes did the same thing — a wall and a
// reviewer's verdict — because every one of those sentences was a template
// written without ever reading the record it was describing. A caller believed
// it; worse, a downstream judge reading the outcome text would learn the run
// produced nothing.
//
// The rule the fix stands on: a run's own account of itself is not evidence
// about what it produced. Where the record and the account disagree, both are
// said, and the record is named — never a blanket nothing-here over files that
// exist. The last case is the guard against over-correcting: when nothing
// really was produced, the blunt sentence is the honest one.
func TestTheClosingNarrationCannotContradictTheArtifacts(t *testing.T) {
	root := t.TempDir()
	graph, err := store.Open(filepath.Join(root, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	delivered := make([]string, 0, 2)
	for _, name := range []string{"brief-one.md", "brief-two.md"} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("the delivered brief\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		delivered = append(delivered, path)
	}
	files := "\n\nFiles:\n" + strings.Join(delivered, "\n")
	receipt := "\n\n[" + resident.OverrunContinuationMessage(2) + "]"

	for _, shape := range []struct {
		name string
		// leaf is what the work said as it landed — the only durable record of
		// what reached disk. failure, when set, is the verdict written over it
		// on the node the caller reads.
		leaf    string
		summary string
		failure string
		// wrote says the files exist on disk for this shape. The last shape
		// runs the same path with an empty record.
		wrote bool
	}{
		{name: "ended inside a split", summary: "wrote what it had" + files + receipt, wrote: true},
		{name: "the reviewer called it a failure",
			leaf:    "wrote what it had" + files,
			failure: "the review found nothing usable here; none of this is the answer.", wrote: true},
		{name: "nothing was produced", summary: "it never got started" + receipt},
	} {
		t.Run(shape.name, func(t *testing.T) {
			session := "grounded-" + strings.ReplaceAll(shape.name, " ", "-")
			command, err := graph.RequestCommand(store.Command{
				SessionID: session, Kind: store.CommandSplice, Instruction: "write the briefs",
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := graph.ResolveCommand(command.Seq, store.CommandApplied, "spliced 1 node"); err != nil {
				t.Fatal(err)
			}
			node := "task-" + session
			if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
				{ID: node, Brief: "write the briefs", Stage: 0},
			}}, store.Provenance{Origin: store.OriginUser, SessionID: session,
				Intent: "write the briefs"}); err != nil {
				t.Fatal(err)
			}
			if shape.leaf != "" {
				// The work that actually wrote the files, landed under the node
				// the caller reads. This is the shape the defect had: the record
				// is one hop away from the account of it.
				leafID := node + "-1"
				if err := graph.Splice(node, store.Subtree{Nodes: []store.NodeSpec{
					{ID: leafID, Brief: "write the briefs", Stage: 0},
				}}, store.Provenance{Origin: store.OriginUser, SessionID: session,
					Intent: "write the briefs"}); err != nil {
					t.Fatal(err)
				}
				leafClaim, claimed, err := graph.Claim(leafID, "test")
				if err != nil || !claimed {
					t.Fatalf("claim leaf: %v (claimed=%v)", err, claimed)
				}
				if err := graph.Start(leafClaim); err != nil {
					t.Fatal(err)
				}
				if err := graph.Complete(leafClaim, shape.leaf); err != nil {
					t.Fatal(err)
				}
			}
			claim, claimed, err := graph.Claim(node, "test")
			if err != nil || !claimed {
				t.Fatalf("claim: %v (claimed=%v)", err, claimed)
			}
			if err := graph.Start(claim); err != nil {
				t.Fatal(err)
			}
			if shape.failure != "" {
				// The reviewer's verdict, recorded the way a judged failure is:
				// the account the caller reads says nothing survived, while the
				// leaf under it says what it wrote and the files are there.
				if err := graph.Fail(claim, shape.failure); err != nil {
					t.Fatal(err)
				}
			} else if err := graph.Complete(claim, shape.summary); err != nil {
				t.Fatal(err)
			}

			watcher := &settlementWatch{
				graph: graph, session: session, commandSeq: command.Seq,
				refused: make(chan planEstimate, 1), started: time.Now(),
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			outcome, err := watcher.wait(ctx)
			if err != nil {
				t.Fatal(err)
			}
			assertErrandIsHonest(t, outcome, errandStatus(outcome))

			if !shape.wrote {
				if len(outcome.Artifacts) != 0 {
					t.Fatalf("a run that wrote nothing reported files: %+v", outcome)
				}
				if !strings.Contains(outcome.Deliverable, "Nothing here is the answer") {
					t.Fatalf("an empty run lost its plain sentence: %q", outcome.Deliverable)
				}
				return
			}
			if len(outcome.Artifacts) != len(delivered) {
				t.Fatalf("the record lost files: %+v", outcome.Artifacts)
			}
			for _, path := range delivered {
				if !strings.Contains(outcome.Deliverable, path) {
					t.Fatalf("the closing line never named %s:\n%s", path, outcome.Deliverable)
				}
			}
			if strings.Contains(outcome.Deliverable, "Nothing here is the answer") {
				t.Fatalf("the closing line denied files that exist:\n%s", outcome.Deliverable)
			}
		})
	}
}

// The two window dials a harness sets per run. They exist because the context
// law is read from the environment by four kinds of caller that share no config
// object, so the flags write the environment once rather than threading a
// budget down through the build — and a run that names neither must leave the
// shell's own exports exactly as it found them, or a campaign that pinned the
// window in its wrapper script would silently be overridden per errand.
func TestTheContextFlagsSetTheWindowLawAndSilenceLeavesItAlone(t *testing.T) {
	t.Setenv("AFORGE_CONTEXT_FILL_PCT", "42")
	t.Setenv("AFORGE_COMPLETION_RESERVE", "4242")

	if err := applyContextLaw(0, 0); err != nil {
		t.Fatal(err)
	}
	if fill, reserve := ctxbudget.FillPercent(), ctxbudget.CompletionReserve(); fill != 42 || reserve != 4242 {
		t.Fatalf("a run that named no flag rewrote the environment: fill=%d reserve=%d", fill, reserve)
	}

	if err := applyContextLaw(80, 32768); err != nil {
		t.Fatal(err)
	}
	if fill, reserve := ctxbudget.FillPercent(), ctxbudget.CompletionReserve(); fill != 80 || reserve != 32768 {
		t.Fatalf("the flags did not reach the law: fill=%d reserve=%d", fill, reserve)
	}

	// Clamping is the law's own job (a typo may neither starve nor overrun a
	// window); refusing a negative is this command's, because there is no
	// reading of it that was meant.
	if err := applyContextLaw(-1, 0); err == nil {
		t.Fatal("a negative fill was accepted")
	}
	if err := applyContextLaw(0, -1); err == nil {
		t.Fatal("a negative reserve was accepted")
	}
	if err := applyContextLaw(99, 0); err != nil {
		t.Fatal(err)
	}
	if fill := ctxbudget.FillPercent(); fill != 90 {
		t.Fatalf("an over-large fill was not clamped by the law: %d", fill)
	}
}

// The exit code is the verdict, and the verdict has to agree with the page.
//
// Two settled runs put their own shortfall on stdout and then left `0` under it:
// one whose delivery gate rejected the deliverable and stood by the rejection,
// and one that told the caller "Not all of this landed: 1 of 2 parts finished".
// A pipeline reads the code and nothing else, so both were recorded as work that
// stands. They are partials, and 2 is what a partial leaves with.
func TestASettledRunThatDidNotLandWholeLeavesWithAPartialCode(t *testing.T) {
	for _, shape := range []struct {
		name  string
		build func(t *testing.T, graph *store.Store, session string)
		want  exitStatus
	}{
		{
			name: "a part of the job failed",
			build: func(t *testing.T, graph *store.Store, session string) {
				t.Helper()
				spliceForErrand(t, graph, session, []store.NodeSpec{
					{ID: "task-1", Brief: "reconcile the ledgers"},
					{ID: "task-1-n1", Parent: "task-1", Brief: "read the bank export"},
					{ID: "task-1-n2", Parent: "task-1", Brief: "read the invoices"},
				})
				settleNode(t, graph, "task-1-n1", "the export is read", "")
				settleNode(t, graph, "task-1-n2", "", "the invoice API answers 410 Gone")
				settleNode(t, graph, "task-1", "Here is the reconciliation.", "")
			},
			want: exitPartial,
		},
		{
			name: "the gate stood by its rejection",
			build: func(t *testing.T, graph *store.Store, session string) {
				t.Helper()
				spliceForErrand(t, graph, session, []store.NodeSpec{{ID: "task-1", Brief: "write the release note"}})
				settleNode(t, graph, "task-1", "RELEASE NOTE DRAFT: the parser is faster.", "")
				if err := graph.RecordDeliveryGate("task-1", store.DeliveryGate{
					Gap: "the migration steps the ask named are not in it", Quote: "with migration steps",
				}); err != nil {
					t.Fatal(err)
				}
			},
			want: exitPartial,
		},
		{
			// The measured shape: a review found the deliverable empty, the
			// repair that would have filled it was refused for want of rounds,
			// and the run left with exit 0 because the refusal sentence was
			// read as the gate correcting itself. The gap was never closed;
			// only the repair was refused.
			name: "the gap still stands and only the repair was refused",
			build: func(t *testing.T, graph *store.Store, session string) {
				t.Helper()
				spliceForErrand(t, graph, session, []store.NodeSpec{{ID: "task-1", Brief: "write the spacing ladder"}})
				settleNode(t, graph, "task-1", "I'm handing this over with a reservation.", "")
				if err := graph.RecordDeliveryGate("task-1", store.DeliveryGate{
					Gap:      "Deliverable is empty - contains no implementation",
					Refused:  "no more work could be started on it",
					Unclosed: true,
				}); err != nil {
					t.Fatal(err)
				}
			},
			want: exitPartial,
		},
		{
			// A refusal that checked NOTHING IN THE WORLD. It declines to buy a
			// round over where the review got its words; it settles nothing
			// about whether the work landed, and the finding is still standing
			// when the run hands over. Seven of eight measured DeepSWE runs
			// exited 0 on exactly this shape, each with a review that was right
			// (bench/deepswe/AUTOPSY.md).
			name: "the gap was refused for where its words came from",
			build: func(t *testing.T, graph *store.Store, session string) {
				t.Helper()
				spliceForErrand(t, graph, session, []store.NodeSpec{{ID: "task-1", Brief: "write the release note"}})
				settleNode(t, graph, "task-1", "RELEASE NOTE: the parser is faster.", "")
				if err := graph.RecordDeliveryGate("task-1", store.DeliveryGate{
					Gap:     "it does not benchmark the parser",
					Refused: "that is not in the request",
				}); err != nil {
					t.Fatal(err)
				}
			},
			want: exitPartial,
		},
		{
			// And the refusal that DID check the world keeps exiting 0: the file
			// the review says is missing is on disk under the name the request
			// used, so the review lost on evidence and the delivery stands.
			// Charging this a non-zero code would teach a harness to distrust
			// the gate's own corrections.
			name: "the gap was overturned against the world",
			build: func(t *testing.T, graph *store.Store, session string) {
				t.Helper()
				spliceForErrand(t, graph, session, []store.NodeSpec{{ID: "task-1", Brief: "write the release note"}})
				settleNode(t, graph, "task-1", "RELEASE NOTE: the parser is faster.", "")
				if err := graph.RecordDeliveryGate("task-1", store.DeliveryGate{
					Gap:        "notes.md was never written",
					Refused:    "what it asked for is already on disk under the name the request used",
					Overturned: true,
				}); err != nil {
					t.Fatal(err)
				}
			},
			want: 0,
		},
		{
			name: "the polish pass closed the gap",
			build: func(t *testing.T, graph *store.Store, session string) {
				t.Helper()
				spliceForErrand(t, graph, session, []store.NodeSpec{{ID: "task-1", Brief: "write the release note"}})
				settleNode(t, graph, "task-1", "RELEASE NOTE FINAL: faster parser, and the migration steps.", "")
				if err := graph.RecordDeliveryGate("task-1", store.DeliveryGate{
					Gap: "the migration steps the ask named are not in it", PolishClosed: true,
				}); err != nil {
					t.Fatal(err)
				}
			},
			want: 0,
		},
		{
			name: "the whole of it landed",
			build: func(t *testing.T, graph *store.Store, session string) {
				t.Helper()
				spliceForErrand(t, graph, session, []store.NodeSpec{
					{ID: "task-1", Brief: "reconcile the ledgers"},
					{ID: "task-1-n1", Parent: "task-1", Brief: "read the bank export"},
				})
				settleNode(t, graph, "task-1-n1", "the export is read", "")
				settleNode(t, graph, "task-1", "Here is the reconciliation.", "")
			},
			want: 0,
		},
	} {
		t.Run(shape.name, func(t *testing.T) {
			graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer graph.Close()
			session := "headless-verdict"
			command, err := graph.RequestCommand(store.Command{
				SessionID: session, Kind: store.CommandSplice, Instruction: "do the thing",
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := graph.ResolveCommand(command.Seq, store.CommandApplied, "spliced"); err != nil {
				t.Fatal(err)
			}
			shape.build(t, graph, session)

			watcher := &settlementWatch{
				graph: graph, session: session, commandSeq: command.Seq,
				refused: make(chan planEstimate, 1), started: time.Now(),
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			outcome, err := watcher.wait(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !outcome.Settled {
				t.Fatalf("the errand never settled: %+v", outcome)
			}
			assertErrandIsHonest(t, outcome, errandStatus(outcome))
			if outcome.status != shape.want {
				t.Fatalf("exit %d, wanted %d: %+v", outcome.status, shape.want, outcome)
			}
			// Whatever the verdict, the work that did land is still handed over.
			if strings.TrimSpace(outcome.Deliverable) == "" {
				t.Fatalf("a settled run reported nothing at all: %+v", outcome)
			}
		})
	}
}

// spliceForErrand admits one subtree under the spine on this errand's session.
func spliceForErrand(t *testing.T, graph *store.Store, session string, nodes []store.NodeSpec) {
	t.Helper()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: nodes}, store.Provenance{
		Origin: store.OriginUser, SessionID: session, Intent: "do the thing",
	}); err != nil {
		t.Fatal(err)
	}
}

// settleNode runs one node to its ending the way a worker does: claim, start,
// and either a summary or the reason it failed.
func settleNode(t *testing.T, graph *store.Store, id, summary, failure string) {
	t.Helper()
	claim, claimed, err := graph.Claim(id, "test")
	if err != nil || !claimed {
		t.Fatalf("claim %s: %v (claimed=%v)", id, err, claimed)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if failure != "" {
		if err := graph.Fail(claim, failure); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := graph.Complete(claim, summary); err != nil {
		t.Fatal(err)
	}
}

// One list, once. A worker names the files it wrote at the end of its summary,
// and the errand prints the same paths under it as its own footer — so stdout
// carried the identical absolute path twice, back to back, on every run that
// produced a file. The footer is the home: it is structured, it is what --json
// carries, and it is there whether or not a worker thought to mention anything.
//
// The graph keeps its copy. A node downstream of this one is handed the files
// its dependency produced by reading absolute paths out of that summary, so the
// block comes off on the way to stdout and nowhere earlier.
func TestTheFileListReachesStdoutOnceAndStaysInTheGraph(t *testing.T) {
	script := newScriptedBrain(t)
	script.writeFile = true
	script.gatePasses = true
	defer script.close()

	database := filepath.Join(t.TempDir(), "graph.db")
	workspace := t.TempDir()
	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task: "write the release note and include the migration steps", workspace: workspace,
		database: database, timeout: 60 * time.Second,
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	}); err != nil {
		t.Fatalf("errand: %v\n%s", err, stderr.String())
	}
	written := filepath.Join(workspace, artifactName)
	if got := strings.Count(stdout.String(), written); got != 1 {
		t.Fatalf("the file was named %d times on stdout, want once:\n%s", got, stdout.String())
	}
	if strings.Contains(stdout.String(), "Files:") {
		t.Fatalf("the worker's own file list survived onto stdout:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "files:") {
		t.Fatalf("the footer never listed the file:\n%s", stdout.String())
	}

	graph, err := store.Open(database)
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	nodes, err := graph.SubtreeNodes(store.RootID)
	if err != nil {
		t.Fatal(err)
	}
	kept := false
	for _, node := range nodes {
		if strings.Contains(node.Summary, "Files:") && strings.Contains(node.Summary, written) {
			kept = true
		}
	}
	if !kept {
		t.Fatal("the summary the graph keeps lost the paths a dependent node reads out of it")
	}
}

// artifacts is what this run produced, and a path is not produced by being
// mentioned. Reading it back out of a worker's prose credited the run with
// every real file the worker happened to name — and credited it with nothing at
// all when the worker wrote "hello.txt" instead of the whole path, which is how
// a run that wrote a file reported artifacts: []. The workspace's own registry
// is the record; the prose is the fallback for a run this process handed to a
// resident and never saw the workers of.
func TestArtifactsAreWhatTheRunProducedNotEveryPathItMentioned(t *testing.T) {
	script := newScriptedBrain(t)
	script.writeFile = true
	script.gatePasses = true
	defer script.close()

	workspace := t.TempDir()
	// A file that was already there. The person's own material, read by the
	// worker and named in its answer, and produced by nobody.
	mentioned := filepath.Join(workspace, "changelog.md")
	if err := os.WriteFile(mentioned, []byte("the parser got faster\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	script.namesPath = mentioned

	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task: "write the release note and include the migration steps", workspace: workspace,
		asJSON: true, timeout: 60 * time.Second,
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	}); err != nil {
		t.Fatalf("errand: %v\n%s", err, stderr.String())
	}
	var outcome struct {
		Artifacts []string `json:"artifacts"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &outcome); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, stdout.String())
	}
	written := filepath.Join(workspace, artifactName)
	if len(outcome.Artifacts) != 1 || outcome.Artifacts[0] != written {
		t.Fatalf("artifacts = %v, want only the file the run wrote (%s)", outcome.Artifacts, written)
	}
}

// The store has to agree with the exit code. A run that left with 0 and left a
// node of its own still moving would be telling two different stories about the
// same work, and the durable one is the one anybody debugging reads.
//
// The permanent spine is the deliberate exception and is asserted as such: the
// root is Running by construction, forever, and the store repairs it back to
// Running if anything ever closes it. It is not this errand's node and never
// settles with it.
func TestASettledErrandLeavesNoNodeOfItsOwnStillRunning(t *testing.T) {
	script := newScriptedBrain(t)
	script.gatePasses = true
	defer script.close()

	database := filepath.Join(t.TempDir(), "graph.db")
	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task:     "write the release note and include the migration steps",
		database: database, timeout: 60 * time.Second,
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	}); err != nil {
		t.Fatalf("errand: %v\n%s", err, stderr.String())
	}
	graph, err := store.Open(database)
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	nodes, err := graph.SubtreeNodes(store.RootID)
	if err != nil {
		t.Fatal(err)
	}
	owned := 0
	for _, node := range nodes {
		if node.ID == store.RootID {
			if node.Status != store.Running {
				t.Fatalf("the permanent spine settled with the errand: %s", node.Status)
			}
			continue
		}
		owned++
		if !terminalStatus(node.Status) {
			t.Fatalf("%s is still %s after a run that left with exit 0", node.ID, node.Status)
		}
	}
	if owned == 0 {
		t.Fatal("the errand left no node of its own behind at all")
	}
}

// A one-shot schedules nothing for later. Self-practice is the resident's own
// curiosity, and it was firing inside every headless run — writing a
// practice-loop charter and its work into whatever store the run was pointed
// at, including a person's own with --db. It costs almost nothing and it is not
// the errand's, which is reason enough.
func TestAHeadlessErrandSchedulesNoPractice(t *testing.T) {
	script := newScriptedBrain(t)
	script.gatePasses = true
	defer script.close()
	// The harness zeroes the practice budget for every other test here. This is
	// the one run that must prove the gate rather than the setting.
	t.Setenv("AFORGE_PRACTICE_BUDGET", "2")

	database := filepath.Join(t.TempDir(), "graph.db")
	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task:     "write the release note and include the migration steps",
		database: database, timeout: 60 * time.Second,
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	}); err != nil {
		t.Fatalf("errand: %v\n%s", err, stderr.String())
	}
	graph, err := store.Open(database)
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	charters, err := graph.Charters()
	if err != nil {
		t.Fatal(err)
	}
	for _, charter := range charters {
		if strings.Contains(charter.ID, "practice") ||
			charter.ProposalShape == store.PracticeCharterShape {
			t.Fatalf("a one-shot errand stood up %q in the person's own store", charter.ID)
		}
	}
	nodes, err := graph.SubtreeNodes(store.RootID)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if node.Group == store.PracticeGroup || strings.Contains(node.ID, "practice") {
			t.Fatalf("a one-shot errand left practice work behind: %s", node.ID)
		}
	}
}
