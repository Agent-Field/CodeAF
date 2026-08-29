package main

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/revision"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
)

// The meter was broken on the path that carries almost all the tokens. The
// executor counted cache reads correctly, the runner journalled whatever the
// result carried, and the three literals between them copied prompt, completion
// and cost and dropped the cached count — so leaf rows read as cold runs on
// every job, and cache discipline was unfalsifiable from the one table anybody
// audits. This runs the whole seam: what a leaf spent, through the runner, into
// the journal.
func TestALeafsCachedTokensReachTheJournal(t *testing.T) {
	graph := openCacheStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "measure the cache", Title: "Cache"},
		{ID: "job-n1", Parent: "job", Brief: "ride a warm prefix", Title: "Leaf"},
	}}, store.Provenance{
		Origin: store.OriginUser, SessionID: "meter", Intent: "measure the cache",
	}); err != nil {
		t.Fatal(err)
	}
	// The room the work was asked for in: what SessionSpend sums is bounded by
	// the ask, so the ask has to exist.
	if _, err := thread.Post(graph, store.Message{
		SessionID: "meter", Role: store.RoleUser, NodeID: store.RootID, Body: "measure the cache",
	}); err != nil {
		t.Fatal(err)
	}

	spent := exec.Usage{PromptTokens: 1_877_154, CompletionTokens: 4_000, CachedTokens: 1_750_000, Cost: 0.42}
	// The same spend with its shape kept. Three turns, each re-sending what the
	// one before it left: the totals are identical and the story is not, which
	// is the whole reason the ledger exists.
	shape := []exec.TurnUsage{
		{Turn: 1, Usage: exec.Usage{Calls: 1, PromptTokens: 600_000, CompletionTokens: 1_500,
			CachedTokens: 550_000, Cost: 0.14}, Sent: 600_000},
		{Turn: 2, Usage: exec.Usage{Calls: 1, PromptTokens: 620_000, CompletionTokens: 1_500,
			CachedTokens: 600_000, Cost: 0.14}, Sent: 620_000},
		{Turn: 3, Usage: exec.Usage{Calls: 1, PromptTokens: 657_154, CompletionTokens: 1_000,
			CachedTokens: 600_000, Cost: 0.14}, Sent: 657_154},
	}
	runner := resident.NewRunner(graph, func(context.Context, store.Node) (resident.ExecResult, error) {
		result := leafSpend(spent, shape, "worker/model")
		result.Summary = "done"
		return result, nil
	}, "meter-runner", 1)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.Wait()

	room, found, err := graph.SessionSpend("meter")
	if err != nil || !found {
		t.Fatalf("session spend: found=%t err=%v", found, err)
	}
	if room.Work.CachedTokens != spent.CachedTokens {
		t.Fatalf("the journal carries %d cached tokens, want %d — the leaf row reads as a cold run",
			room.Work.CachedTokens, spent.CachedTokens)
	}
	if room.Work.PromptTokens != spent.PromptTokens || room.Work.CompletionTokens != spent.CompletionTokens {
		t.Fatalf("the rest of the ledger moved with it: %+v", room.Work)
	}

	// The other half of the same seam: the shape underneath the total. A summed
	// row cannot say whether 1.88M prompt tokens were three enormous turns or a
	// hundred and eleven ordinary ones, and those are opposite findings.
	turns, err := graph.TurnUsageFor("job-n1")
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != len(shape) {
		t.Fatalf("the journal kept %d turn rows, want %d", len(turns), len(shape))
	}
	var prompt, completion, cached, sent int
	var cost float64
	for index, turn := range turns {
		if turn.Turn != index+1 {
			t.Fatalf("turn %d is numbered %d; the ledger must be in order and one-based", index, turn.Turn)
		}
		prompt += turn.PromptTokens
		completion += turn.CompletionTokens
		cached += turn.CachedTokens
		sent += turn.SentTokens
		cost += turn.Cost
	}
	if prompt != spent.PromptTokens || completion != spent.CompletionTokens || cached != spent.CachedTokens {
		t.Fatalf("the turn rows sum to prompt=%d completion=%d cached=%d, and the node row says %+v",
			prompt, completion, cached, spent)
	}
	if math.Abs(cost-spent.Cost) > 1e-9 {
		t.Fatalf("the turn rows sum to $%.6f, and the node row says $%.6f", cost, spent.Cost)
	}
	// Cumulative context pressure — Σ over turns of what went on the wire — is
	// the quantity no other row in the journal carries.
	if sent != spent.PromptTokens {
		t.Fatalf("the ledger records %d tokens sent across the run, want %d", sent, spent.PromptTokens)
	}
}

// The gate failed a delivery for not containing a script's text while the
// script sat on disk, then bought a continuation node that retyped it. The
// judge is now handed what the request named and what the run produced, and a
// gap quoting the ask's own filename is closed by the file rather than by
// another round of work.
func TestTheGateWeighsTheProducedFileBeforeTheProse(t *testing.T) {
	dir := t.TempDir()
	produced := filepath.Join(dir, "docs", "decision-memo.md")
	if err := os.MkdirAll(filepath.Dir(produced), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(produced, []byte("the memo itself"), 0o600); err != nil {
		t.Fatal(err)
	}

	node := store.Node{
		ID: "job", Brief: "write the memo",
		Provenance: store.Provenance{Intent: "write docs/decision-memo.md, a rigorous decision memo"},
	}
	outcome := &exec.Outcome{Ran: []string{`write {"path":"docs/decision-memo.md"}`}}
	spec := plan.Spec{Done: plan.Done{
		Produces:   []string{"docs/decision-memo.md"},
		Conditions: []plan.Check{{Kind: plan.CheckRead, Check: "open the memo", Expect: "it states a decision"}},
	}}
	record := gateEvidence(node, spec, outcome, []string{produced}, true)

	settings := config.Config{Model: "worker/model"}
	capture := &gateCaptureClient{model: "worker/model"}
	revision.JudgeDeliverable(context.Background(), settings,
		adoptLiveClient(settings, capture.model, capture), openCacheStore(t), node,
		"I wrote the memo to docs/decision-memo.md.", "", record, "worker/model")
	body := capture.messages[len(capture.messages)-1].Content[0].Text
	for name, want := range map[string]string{
		"the request's own name":     "docs/decision-memo.md — produced, at " + produced,
		"the criterion it was given": "It produces: docs/decision-memo.md",
		"and the check that settles": "- (read) open the memo — it states a decision",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the gate was not handed %s: %q missing from\n%s", name, want, body)
		}
	}

	// And the behavioural half: a gap quoting the span of the ask that names
	// the produced file buys nothing. It is delivered under, not acted on.
	if refusal := revision.AdmitGapArtifact([]string{"write docs/decision-memo.md"}, record); refusal == "" {
		t.Fatal("a gap quoting the ask's own filename still bought a round while the file sat on disk")
	}
	// A gap about the substance inside the file is not a filename and is
	// judged the ordinary way — the refusal must not swallow it.
	if refusal := revision.AdmitGapArtifact([]string{"a rigorous decision memo"}, record); refusal != "" {
		t.Fatalf("a gap about the substance was refused as already-produced: %q", refusal)
	}
}

// The leaf that claimed "the file is written and verified" and never wrote it.
// Its hint path had become a directory, so it wrote the deliverable inside and
// the asked-for file existed nowhere. Both halves are closed here: the hint is
// withdrawn rather than offered as an address it cannot be, and the record the
// gate reads says in words that nothing of the asked-for name was produced.
func TestAHintPathThatIsADirectoryIsNeitherOfferedNorClaimedAsWritten(t *testing.T) {
	root := t.TempDir()
	space, err := exec.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	node := store.Node{
		ID: "job", Parent: store.RootID, Title: "Write docs/decision-memo.md, a rigorous",
		Provenance: store.Provenance{Intent: "write ./docs/decision-memo.md — a rigorous decision memo"},
	}
	hint, _ := leafOutputHint(node, node.Title, space)
	if hint == "" {
		t.Fatal("the root leaf was offered no output path at all")
	}
	if err := os.MkdirAll(filepath.Join(root, hint), 0o755); err != nil {
		t.Fatal(err)
	}
	if occupied, _ := leafOutputHint(node, node.Title, space); occupied != "" {
		t.Fatalf("a directory stands at the hint path and it was offered anyway: %q", occupied)
	}

	// What the leaf did instead: the right content at the wrong address.
	inside := filepath.Join(root, hint, "decision-memo.md")
	if err := os.WriteFile(inside, []byte("1,146 words of correct, on-topic memo"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := gateEvidence(node, plan.Spec{}, &exec.Outcome{Ran: []string{`write`}},
		[]string{inside, filepath.Join(root, hint)}, true)

	settings := config.Config{Model: "worker/model"}
	capture := &gateCaptureClient{model: "worker/model"}
	revision.JudgeDeliverable(context.Background(), settings,
		adoptLiveClient(settings, capture.model, capture), openCacheStore(t), node,
		"The file is written and verified.", "", record, "worker/model")
	body := capture.messages[len(capture.messages)-1].Content[0].Text
	if !strings.Contains(body, "docs/decision-memo.md — nothing of that name is among what was left behind") {
		t.Fatalf("the gate was not told the asked-for file was never written:\n%s", body)
	}
	if !strings.Contains(body, filepath.Join(root, hint)+" (a directory, not a file)") {
		t.Fatalf("a directory recorded as an artifact still reads as a written file:\n%s", body)
	}
	// And nothing here excuses the claim: the gap is the file, so the refusal
	// that closes a produced-file gap must not fire.
	if refusal := revision.AdmitGapArtifact([]string{"write ./docs/decision-memo.md"}, record); refusal != "" {
		t.Fatalf("an unwritten file was taken as already produced: %q", refusal)
	}
}

// A coding leaf is the one worker that knows what was already broken when it
// arrived, and the gate is the reader that cannot possibly find out for itself.
func TestGateEvidenceCarriesTheBaselineDelta(t *testing.T) {
	note := "`make all` exited 2, and every failing test it reports " +
		"(TestFailGenFishCompletionFile) was ALREADY failing at this commit."
	record := gateEvidence(store.Node{ID: "job"}, plan.Spec{},
		&exec.Outcome{Ran: []string{"stage verification pass"}, Baseline: []string{note}},
		nil, true)
	if len(record.Baseline) != 1 || record.Baseline[0] != note {
		t.Fatalf("the gate record dropped the baseline delta: %#v", record.Baseline)
	}
	// A worker that measured nothing claims nothing.
	plain := gateEvidence(store.Node{ID: "job"}, plan.Spec{}, &exec.Outcome{}, nil, true)
	if len(plain.Baseline) != 0 {
		t.Fatalf("a leaf with no baseline claimed one: %#v", plain.Baseline)
	}
}

// The gate judges the node under the root, and for a divided job that is the
// assembly: a summary written over the workers' results, which itself writes
// nothing. Judged on its own artifacts it failed a delivery with seven green
// packages on the disk and bought a second wave of all seven. It is shown
// what the results it assembled were handed on with, once each, own files
// first.
func TestTheGateSeesWhatTheAssembledResultsLeftBehind(t *testing.T) {
	dir := t.TempDir()
	own := filepath.Join(dir, "summary.md")
	theirs := filepath.Join(dir, "luhn", "tests", "test_luhn.py")
	if err := os.MkdirAll(filepath.Dir(theirs), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{own, theirs} {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inputs := []exec.Input{
		{Title: "task-2-n7", Result: "luhn done", Artifacts: []string{theirs, own}},
		{Title: "task-2-n1", Result: "csvstats done", Artifacts: []string{theirs}},
	}
	got := assembledArtifacts([]string{own}, inputs)
	want := []string{own, theirs}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("assembled = %v, want %v", got, want)
	}
	record := gateEvidence(store.Node{ID: "task-2"}, plan.Spec{}, &exec.Outcome{}, got, true)
	if len(record.Artifacts) != 2 {
		t.Fatalf("the gate was shown %v, want both files", record.Artifacts)
	}
}
