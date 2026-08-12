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

	"github.com/Agent-Field/aforge-v2/internal/config"
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
	if err := doErrand(doRequest{
		task:      "write the release note and include the migration steps",
		timeout:   60 * time.Second,
		stdout:    &stdout,
		stderr:    &stderr,
		newClient: script.client,
	}); err != nil {
		t.Fatalf("the errand did not settle cleanly: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
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
	if !asExitStatus(err, &status) || status != exitTimeout {
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

// A chat window is the other half of the same seam and must not have moved.
// One thread hosts many unrelated jobs, so each still gets its own directory
// under the store's workspace; only an errand shares one.
func TestChatKeepsItsPerJobWorkspaceLayout(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	root := t.TempDir()
	window := testWindow(t, root)
	brain, err := buildBrain(window, "s1", brainOptions{newClient: script.client})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(brain.closeAll)
	if brain.workspaceRoot != filepath.Join(root, "workspace") {
		t.Fatalf("chat workspace root = %q, want the store's own", brain.workspaceRoot)
	}
	// The commander resolves a node to its own job directory beneath that root,
	// which is the layout the whole chat surface reads through.
	if err := window.graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "produce the artifact", Stage: 0},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "produce the artifact"}); err != nil {
		t.Fatal(err)
	}
	jobDir := filepath.Join(brain.workspaceRoot, "job")
	if err := os.MkdirAll(jobDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if got, ok := brain.commander.WorkspacePath("job"); !ok || got != jobDir {
		t.Fatalf("chat job workspace = (%q, %v), want (%q, true)", got, ok, jobDir)
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

// The factoring itself: one construction, two shapes. A chat window still gets
// every piece it ever had, and headless differs by exactly the conversational
// half — no head, no commander, no stream, no arrival brief — over an
// identically wired reconciler, runner and consent desk.
func TestBuildBrainSeparatesTheConversationFromTheWork(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()

	for _, shape := range []struct {
		name     string
		headless bool
	}{{"chat", false}, {"headless", true}} {
		t.Run(shape.name, func(t *testing.T) {
			window := testWindow(t, t.TempDir())
			brain, err := buildBrain(window, "s1", brainOptions{
				headless: shape.headless, ephemeral: shape.headless, newClient: script.client,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(brain.closeAll)
			// The work half is the same object either way. This is the whole
			// claim the headless mode rests on.
			if brain.reconciler == nil || brain.runner == nil || brain.consent == nil {
				t.Fatal("the working half is incomplete")
			}
			if shape.headless {
				if brain.serveHead != nil || brain.commander != nil ||
					brain.streamEvents != nil || brain.deliverBrief != nil {
					t.Fatal("a headless brain carries a conversation it cannot have")
				}
				return
			}
			if brain.serveHead == nil || brain.commander == nil || brain.streamEvents == nil {
				t.Fatal("a chat brain lost part of its conversation to the factoring")
			}
		})
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

// The other half of the contract: a question the errand's own semantics cannot
// answer must not end the run in silence. Three seconds, five thousandths of a
// cent and an empty stdout is indistinguishable from a fast cheap success in a
// pipeline, which is exactly how the original defect went unnoticed.
//
// So it fails loudly: the question verbatim on stderr, a line saying headless
// mode cannot answer it, exit 1, and a JSON object whose blocked_on carries the
// question while deliverable stays empty.
func TestDoFailsLoudlyOnAQuestionItCannotAnswer(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	script.compilerAsks = unanswerableQuestion

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task:    "reconcile the two ledgers and tell me what is wrong",
		asJSON:  true,
		timeout: 60 * time.Second, workspace: t.TempDir(),
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
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
	outcome := decodeErrand(t, stdout.String())
	if !strings.Contains(outcome.BlockedOn, unanswerableQuestion) {
		t.Fatalf("blocked_on does not carry the question: %+v", outcome)
	}
	if strings.TrimSpace(outcome.Deliverable) != "" {
		t.Fatalf("the question polluted the deliverable: %q", outcome.Deliverable)
	}
	assertErrandIsHonest(t, outcome, err)
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
	// --yes-spend covers. Nobody is here, so the run must say so and leave.
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
	// inventedGap makes the gate fail the deliverable against a standard
	// nobody asked for — the shape of the one measured round that made a
	// deliverable worse.
	inventedGap bool
	// writeFile makes the first leaf write a real artifact.
	writeFile bool
	// editPath names a file already in the workspace that the first leaf edits
	// in place, which is what a coding errand actually does.
	editPath string
	// leafCost is what each call reports spending, which is what the consent
	// desk's estimate is built from.
	leafCost float64
	// compileDraftsCharter makes the ORDINARY intent compiler hand back a
	// charter, which is the shape a provider emitting an uninvited key produces
	// — the case the structural pin cannot catch and the reconciler must.
	compileDraftsCharter bool
	// compilerAsks makes the compiler stop on a question instead of compiling.
	compilerAsks string
	// compileSubharness makes the compiler read the ask as one specialist's
	// kind of job, which is what a coding-shaped ask gets from the real one.
	compileSubharness string
	// gatePasses lets a deliverable through on the first look, for the runs
	// whose subject is not the gate.
	gatePasses bool
	// longAnswer, when set, is what the worker hands back instead of the short
	// draft, and the gate passes it on sight. It is how a deliverable longer
	// than any single bound on the path can be followed from the worker's
	// mouth to the person's screen.
	longAnswer string
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
			`"scale":"task","builds_on":[],"assumptions":[],"question":"","trial_of":0,` +
			`"subharness":` + jsonString(s.compileSubharness) + `}`)

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
		if s.longAnswer != "" {
			return s.say(`{"pass":true,"gaps":"","quote":"","exercised":true}`)
		}
		if s.inventedGap {
			// The quote is a span of the compiled goal's own working
			// decisions, not of anything the person typed.
			return s.say(fmt.Sprintf(
				`{"pass":false,"gaps":%q,"quote":%q,"exercised":false}`, inventedGapText, inventedQuote))
		}
		if s.gatePasses {
			return s.say(`{"pass":true,"gaps":"","quote":"","exercised":true}`)
		}
		if s.revisionCloses {
			if round == 1 {
				return s.say(fmt.Sprintf(
					`{"pass":false,"gaps":%q,"quote":%q,"exercised":false}`, gateCritique, citedQuote))
			}
			return s.say(`{"pass":true,"gaps":"","quote":"","exercised":true}`)
		}
		if round <= 2 {
			// The first draft and the revision of it are both judged short of
			// the ask, and the gap quotes the ask itself — the one thing that
			// buys another round of real work.
			return s.say(fmt.Sprintf(
				`{"pass":false,"gaps":"the migration steps are missing","quote":%q,"exercised":false}`, citedQuote))
		}
		return s.say(`{"pass":true,"gaps":"","quote":"","exercised":true}`)

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

// leaf answers as the worker. Which worker it is reads off the inputs it was
// given, which is how the product itself distinguishes the three: a first
// draft, the revision the gate's critique bought, and the work the cited gap
// commissioned.
func (s *scriptedBrain) leaf(body string) string {
	switch {
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
		return s.say(firstDraftAnswer)
	}
}

// jsonString quotes one value for a hand-written body above.
func jsonString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func (s *scriptedBrain) say(content string) string {
	encoded, _ := json.Marshal(content)
	return fmt.Sprintf(`{"model":"scripted","choices":[{"index":0,"finish_reason":"stop",`+
		`"message":{"role":"assistant","content":%s}}],`+
		`"usage":{"prompt_tokens":10,"completion_tokens":10,"total_tokens":20,"cost":%f}}`,
		string(encoded), s.leafCost)
}

func (s *scriptedBrain) tool(name, arguments string) string {
	encoded, _ := json.Marshal(arguments)
	return fmt.Sprintf(`{"model":"scripted","choices":[{"index":0,"finish_reason":"tool_calls",`+
		`"message":{"role":"assistant","content":"","tool_calls":[{"id":"call-1","type":"function",`+
		`"function":{"name":%q,"arguments":%s}}]}}],`+
		`"usage":{"prompt_tokens":10,"completion_tokens":10,"total_tokens":20,"cost":%f}}`,
		name, string(encoded), s.leafCost)
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
		if strings.HasPrefix(line, "store kept at ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "store kept at "))
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
