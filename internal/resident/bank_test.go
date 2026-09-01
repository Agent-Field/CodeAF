package resident

import (
	"encoding/json"
	"strings"
	"testing"

	executor "github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
)

// The incident, at the composition: a leaf that ran a long benchmark campaign,
// shared where it had got to the whole way, wrote its document, and then died on
// its time ceiling with nothing in hand. What it reached must arrive at whoever
// takes over — under the headers the overrun path has always used, because a
// second wording of the same contract is a second contract.
func TestABankIsComposedUnderTheContinuationHeaders(t *testing.T) {
	bank := Bank{
		Shared: []string{
			"implemented all four classification algorithms",
			"benchmarked against RBF-SVM on three datasets",
			"The comparison writeup for all four algorithms is now pulled together into one document",
		},
		Artifacts: []string{"/jobs/craft-4958/04-comparison.md"},
	}

	body := bank.Continuation()
	if !strings.Contains(body, ContinuationPartialHeader) {
		t.Fatalf("the bank did not arrive under the partial header:\n%s", body)
	}
	if !strings.Contains(body, ContinuationFilesHeader) {
		t.Fatalf("the bank did not name its files under the files header:\n%s", body)
	}
	if !strings.Contains(body, BankSharedLead) {
		t.Fatalf("the shared lines arrived without the lead that says they are done:\n%s", body)
	}
	for _, line := range bank.Shared {
		if !strings.Contains(body, line) {
			t.Fatalf("the bank lost a shared line %q:\n%s", line, body)
		}
	}
	if !strings.Contains(body, "/jobs/craft-4958/04-comparison.md") {
		t.Fatalf("the bank lost the file it wrote:\n%s", body)
	}
	// The exact words of the contract, pinned. OverrunGoal writes the same two,
	// and a change to either must be a change to both.
	if ContinuationPartialHeader != "What the previous agent produced before stopping (its partial result arrives as a dependency input; build on it):" {
		t.Fatalf("the partial header moved: %q", ContinuationPartialHeader)
	}
	if ContinuationFilesHeader != "Files already produced, to reuse rather than recreate:" {
		t.Fatalf("the files header moved: %q", ContinuationFilesHeader)
	}
	// And the replan brief still writes them, from here.
	goal := OverrunGoal(store.Node{Brief: "finish the comparison"}, "half of it", []string{"/jobs/x/a.md"}, "", "")
	if !strings.Contains(goal, ContinuationPartialHeader) || !strings.Contains(goal, ContinuationFilesHeader) {
		t.Fatalf("the replan brief stopped sharing the continuation headers:\n%s", goal)
	}
}

// A bank with nothing in it composes nothing. An input announcing an earlier
// attempt that produced no text, said nothing and wrote no file is a sentence
// that costs tokens to tell a worker its own assignment has already failed once.
func TestAnEmptyBankHandsNothingOn(t *testing.T) {
	if !(Bank{}).Empty() {
		t.Fatal("an empty bank did not say so")
	}
	if !(Bank{Shared: []string{"", "   "}}).Empty() {
		t.Fatal("a bank of blank lines is still an empty bank")
	}
	if (Bank{Shared: []string{"wrote the parser"}}).Empty() {
		t.Fatal("a bank holding one shared line called itself empty")
	}
}

// The bank is not a new record. Every line of it is already in the journal, and
// this is the read that finds them: the replaceable progress rows a long worker
// posts, and the notes it shares in its own words.
func TestBankedProgressReadsALeafsOwnRecordBack(t *testing.T) {
	graph := openRunnerStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "craft-4958", Brief: "Deliver the result of ideate novel classification algorithms", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "ideate novel classification algorithms"}); err != nil {
		t.Fatalf("splice: %v", err)
	}

	record(t, graph, "craft-4958", store.Message{
		Role: store.RoleSystem, Body: "benchmarking",
		Progress: &store.MessageProgress{Phase: "benchmarking", Done: 2, Total: 3,
			Latest: "AdaptiveKernel finished on wine and digits"},
	})
	record(t, graph, "craft-4958", store.Message{
		Role: store.RoleAgent,
		Body: NoteMark + "The comparison writeup for all four algorithms is now pulled together into one document",
	})
	// Two rows that are NOT the work reporting itself: a person steering, and
	// the machinery narrating. Neither belongs in a bank.
	record(t, graph, "craft-4958", store.Message{Role: store.RoleUser, Body: "use the same seed everywhere"})
	record(t, graph, "craft-4958", store.Message{Role: store.RoleAgent, Body: "here is your comparison"})

	lines := BankedProgress(graph, "craft-4958")
	if len(lines) != 2 {
		t.Fatalf("banked lines = %q, want the progress row and the shared note only", lines)
	}
	if lines[0] != "AdaptiveKernel finished on wine and digits" {
		t.Fatalf("the progress row lost its latest line: %q", lines[0])
	}
	if lines[1] != "The comparison writeup for all four algorithms is now pulled together into one document" {
		t.Fatalf("the shared note did not come back whole: %q", lines[1])
	}
	if strings.Contains(lines[1], NoteMark) {
		t.Fatalf("the protocol byte leaked into the bank: %q", lines[1])
	}
}

// A craft-rooted leaf is a leaf. The incident WAS a craft root, and the whole
// point of reading the node's own record is that the mechanism cannot tell —
// and must not be able to tell — which namespace an id came from.
func TestACraftRootedLeafBanksExactlyAsATaskLeafDoes(t *testing.T) {
	graph := openRunnerStore(t)
	for _, id := range []string{"craft-4958", "task-4958"} {
		if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
			{ID: id, Brief: "compare four algorithms", Stage: 1},
		}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "compare four algorithms"}); err != nil {
			t.Fatalf("splice %s: %v", id, err)
		}
	}
	for _, id := range []string{"craft-4958", "task-4958"} {
		record(t, graph, id, store.Message{
			Role: store.RoleSystem, Body: "writing",
			Progress: &store.MessageProgress{Phase: "writing", Latest: "the writeup is assembled"},
		})
		record(t, graph, id, store.Message{Role: store.RoleAgent, Body: NoteMark + "three of four datasets finished"})
	}

	crafted := Bank{}.WithShared(BankedProgress(graph, "craft-4958")...).Continuation()
	tasked := Bank{}.WithShared(BankedProgress(graph, "task-4958")...).Continuation()
	if crafted != tasked {
		t.Fatalf("a craft root banked differently from a task leaf:\n--- craft ---\n%s\n--- task ---\n%s", crafted, tasked)
	}
	if !strings.Contains(crafted, "the writeup is assembled") {
		t.Fatalf("the craft root's bank lost its progress:\n%s", crafted)
	}
}

// A leaf claimed a second time is a leaf whose first run was interrupted, and
// the store's own attempt counter is how the runner knows. It is the whole of
// the pickup signal.
func TestALeafRequeuedAtLaunchCarriesAnAttemptCountAndItsBank(t *testing.T) {
	graph := openRunnerStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-1", Brief: "run the campaign", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "run the campaign"}); err != nil {
		t.Fatalf("splice: %v", err)
	}
	claim, ok, err := graph.Claim("task-1", "first-process")
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatalf("start: %v", err)
	}
	record(t, graph, "task-1", store.Message{
		Role: store.RoleSystem, Body: "benchmarking",
		Progress: &store.MessageProgress{Phase: "benchmarking", Latest: "two of three datasets are through"},
	})

	// The process goes away; the next launch sweeps the claim it left behind.
	released, err := graph.ReleaseOrphans()
	if err != nil {
		t.Fatalf("release orphans: %v", err)
	}
	if len(released) != 1 || released[0] != "task-1" {
		t.Fatalf("released = %q, want the interrupted leaf", released)
	}
	node, ok, err := graph.Node("task-1")
	if err != nil || !ok {
		t.Fatalf("read node: ok=%v err=%v", ok, err)
	}
	if node.Status != store.Pending {
		t.Fatalf("status = %v, want pending so the next resident claims it", node.Status)
	}
	// The snapshot the runner hands to execution is read before its own claim,
	// so a leaf that has been here before says so: attempt above zero.
	if node.Attempt == 0 {
		t.Fatalf("attempt = %d, want the first attempt counted so the pickup can be seen", node.Attempt)
	}
	if lines := BankedProgress(graph, "task-1"); len(lines) != 1 ||
		lines[0] != "two of three datasets are through" {
		t.Fatalf("the interrupted leaf's bank did not survive the sweep: %q", lines)
	}
	// And the second claim counts again, so nothing about the sweep resets it.
	second, ok, err := graph.Claim("task-1", "second-process")
	if err != nil || !ok {
		t.Fatalf("second claim: ok=%v err=%v", ok, err)
	}
	_ = second
	again, _, err := graph.Node("task-1")
	if err != nil {
		t.Fatalf("read node: %v", err)
	}
	if again.Attempt <= node.Attempt {
		t.Fatalf("attempt did not increment on the retry: %d then %d", node.Attempt, again.Attempt)
	}
}

func record(t *testing.T, graph *store.Store, nodeID string, message store.Message) {
	t.Helper()
	message.NodeID = nodeID
	if _, err := thread.Record(graph, message); err != nil {
		t.Fatalf("record on %s: %v", nodeID, err)
	}
}

// LeafState is the general-purpose handoff: what a dead leaf's worker actually
// did, derived from its own outcome. A worker with a verifier contributes its
// structured account; every worker contributes the tail of what it did. The
// continuation that knows this resumes from here instead of re-reading
// everything the dead leaf already diagnosed.
func TestLeafStateDerivesFromAccountAndRan(t *testing.T) {
	// A worker with an account: files changed, checks run, last word.
	account := &executor.Account{
		Files: []executor.FileChange{
			{Path: "main.go", Change: executor.ChangeChanged, Added: 10, Removed: 3},
			{Path: "main_test.go", Change: executor.ChangeAdded, Added: 42, Removed: 0},
		},
		Checks: []executor.Check{
			{Command: "go test ./...", Kind: "test", Passed: true},
		},
		Final: "the parser now accepts trailing commas",
	}
	outcome := &executor.Outcome{
		Text:    "partial deliverable",
		Account: account,
		Ran:     []string{`write {"path":"main.go"}`, "bash: go test ./...", "read main_test.go"},
	}
	state := LeafState(outcome)

	// The account's structured file rows must appear.
	if !strings.Contains(state, "What the work changed:") {
		t.Fatalf("the state does not carry the account's file block:\n%s", state)
	}
	if !strings.Contains(state, "main.go") || !strings.Contains(state, "main_test.go") {
		t.Fatalf("the state does not name the changed files:\n%s", state)
	}
	// The account's check results must appear.
	if !strings.Contains(state, "passed: go test ./...") {
		t.Fatalf("the state does not carry the check verdict:\n%s", state)
	}
	// The worker's last calls must appear, so the continuation knows what the
	// dead leaf was doing when it stopped.
	if !strings.Contains(state, "Last calls the worker made, in order:") {
		t.Fatalf("the state does not carry the last calls:\n%s", state)
	}
	if !strings.Contains(state, `write {"path":"main.go"}`) {
		t.Fatalf("the state does not carry the write call:\n%s", state)
	}
}

// A worker with no account — the ordinary generalist — still contributes its
// last calls, which is the one structured record every worker leaves.
func TestLeafStateDerivesFromRanAlone(t *testing.T) {
	outcome := &executor.Outcome{
		Text: "some prose",
		Ran:  []string{"read config.go", "edit config.go", "bash: go build ./..."},
	}
	state := LeafState(outcome)
	if !strings.Contains(state, "Last calls the worker made, in order:") {
		t.Fatalf("a worker with no account still hands on its last calls:\n%s", state)
	}
	if !strings.Contains(state, "read config.go") {
		t.Fatalf("the state does not carry the read call:\n%s", state)
	}
	// No account block, because the worker left none.
	if strings.Contains(state, "What the work changed:") {
		t.Fatalf("a worker with no account should not fabricate one:\n%s", state)
	}
}

// A nil outcome hands on nothing, because there is nothing to hand on.
func TestLeafStateFromNilOutcome(t *testing.T) {
	if state := LeafState(nil); state != "" {
		t.Fatalf("a nil outcome produced state %q, want empty", state)
	}
}

// An outcome with no account and no ran hands on nothing.
func TestLeafStateFromEmptyOutcome(t *testing.T) {
	if state := LeafState(&executor.Outcome{}); state != "" {
		t.Fatalf("an empty outcome produced state %q, want empty", state)
	}
}

// A bank carrying state composes it under the continuation state header, so
// the retry's instruction tells the next agent what the dead one already did.
func TestABankCarriesStateUnderTheContinuationHeader(t *testing.T) {
	bank := Bank{
		Partial:   "half the writeup",
		State:     "What the work changed:\n  2 files changed, +52 -3 lines\n\nLast calls the worker made, in order:\n  write main.go\n  bash: go test ./...",
		Artifacts: []string{"/jobs/x/main.go"},
	}
	body := bank.Continuation()
	if !strings.Contains(body, ContinuationStateHeader) {
		t.Fatalf("the state did not arrive under its header:\n%s", body)
	}
	if !strings.Contains(body, "2 files changed") {
		t.Fatalf("the state's content was lost:\n%s", body)
	}
	// The state sits after the partial and before nothing — the order is
	// partial, files, state, which is the order a reader needs: what it had,
	// what it wrote, what it actually did.
	partialIdx := strings.Index(body, ContinuationPartialHeader)
	stateIdx := strings.Index(body, ContinuationStateHeader)
	if partialIdx < 0 || stateIdx < 0 || stateIdx < partialIdx {
		t.Fatalf("the state must come after the partial:\n%s", body)
	}
}

// A bank with only state and nothing else is not empty, because state alone is
// enough to tell the next agent what the dead one did.
func TestABankWithOnlyStateIsNotEmpty(t *testing.T) {
	bank := Bank{State: "Last calls the worker made, in order:\n  read main.go"}
	if bank.Empty() {
		t.Fatal("a bank holding state called itself empty")
	}
	body := bank.Continuation()
	if !strings.Contains(body, ContinuationStateHeader) {
		t.Fatalf("the state-only bank did not compose:\n%s", body)
	}
}

// OverrunGoal renders the state under the same header, so the re-decomposition
// path — the one that plans the x1/x2 continuation nodes — hands on the dead
// leaf's findings too, not just the retry path.
func TestOverrunGoalCarriesTheDeadLeafsState(t *testing.T) {
	state := "What the work changed:\n  1 file changed, +1 -0 lines\n\nLast calls the worker made, in order:\n  read bug.go\n  edit bug.go"
	goal := OverrunGoal(store.Node{Brief: "fix the bug"}, "partial fix", nil, "", state)
	if !strings.Contains(goal, ContinuationStateHeader) {
		t.Fatalf("the replan goal did not carry the state header:\n%s", goal)
	}
	if !strings.Contains(goal, "1 file changed") {
		t.Fatalf("the state content was lost in the goal:\n%s", goal)
	}
	// An empty state renders nothing — the goal is byte-identical to before.
	bare := OverrunGoal(store.Node{Brief: "fix the bug"}, "partial fix", nil, "", "")
	if strings.Contains(bare, ContinuationStateHeader) {
		t.Fatalf("an empty state rendered a header:\n%s", bare)
	}
}

// The state survives the daily rail: a deferred overrun carries it, and the
// resumed repair hands it to the continuation goal.
func TestDeferredOverrunCarriesStateAcrossTheRail(t *testing.T) {
	state := "Last calls the worker made, in order:\n  edit bug.go"
	deferred := store.DeferredOverrun{
		NodeID: "task-1", Partial: "partial", Prefix: "task-1-x1", State: state,
	}
	encoded, err := json.Marshal(deferred)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded store.DeferredOverrun
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.State != state {
		t.Fatalf("the state did not survive the round trip: %q", decoded.State)
	}
}
