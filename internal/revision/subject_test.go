package revision

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/shaped"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The whole of the run this file exists for, replayed:
// bench/deepswe/results/textual-richlog-follow-state-nvidia-nemotron-3.5-lightning-n1.
// The worker answered the delivery fence with a structured object; the tree held
// the changed sources; and three gates in a row reasoned about the object.
const nemotronDeliverable = `{"contract": "Write the _log.py and _rich_log.py files from scratch implementing FollowChanged, is_following_end and follow_end."}`

// nemotronVerdict is one of the three refusals verbatim, in the shape the gate
// used to accept: a finding about the fenced text, naming no file of the record.
const nemotronVerdict = `{"pass":false,"gaps":"The deliverable is a single JSON contract string, not the required Python source files. The fenced text between BEGIN DELIVERABLE and END DELIVERABLE contains only {\"contract\": \"...\"} — no _log.py, _rich_log.py, or examples/rich_log_follow_state.py.","quote":"Make Log and RichLog expose is_following_end: bool","exercised":false}`

// recordingJudge answers from a script and keeps every prompt it was sent, so a
// test can ask what the gate was actually handed rather than what it concluded.
type recordingJudge struct {
	replies []string
	sent    [][]ai.Message
}

func (r *recordingJudge) CompleteWithMessages(_ context.Context, messages []ai.Message,
	_ ...ai.Option) (*ai.Response, error) {
	r.sent = append(r.sent, messages)
	index := len(r.sent) - 1
	if index >= len(r.replies) {
		index = len(r.replies) - 1
	}
	return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{Role: "assistant",
		Content: []ai.ContentPart{{Type: "text", Text: r.replies[index]}}}}},
		Usage: &ai.Usage{CompletionTokens: 40}}, nil
}

func (r *recordingJudge) Model() string { return "judge/model" }

func (r *recordingJudge) lastPrompt() string {
	messages := r.sent[len(r.sent)-1]
	return messages[len(messages)-1].Content[0].Text
}

// treeFixture is a workspace with a change in it, and the record of that change.
func treeFixture(t *testing.T) (root string, record []string) {
	t.Helper()
	root = t.TempDir()
	files := map[string]string{
		"src/textual/widgets/_log.py":      "class Log:\n    is_following_end = True\n",
		"src/textual/widgets/_rich_log.py": "class RichLog:\n    def follow_end(self):\n        ...\n",
		"tests/test_log.py":                "def test_follow_end():\n    assert True\n",
	}
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		record = append(record, path)
	}
	return root, record
}

func textualNode() store.Node {
	return store.Node{ID: "task-2", Brief: "make the follow state work",
		Provenance: store.Provenance{Intent: "Make Log and RichLog expose is_following_end: bool, " +
			"follow_end(animate: bool = False), and a FollowChanged message."}}
}

// readOnlyFixture is the file named by the measured run's contract and the
// body that must never be mistaken for that run's deliverable.
func readOnlyFixture(t *testing.T) (root, body string) {
	t.Helper()
	root = t.TempDir()
	body = "These are repository instructions the run only read.\n"
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, body
}

// THE GATE JUDGES THE WORLD. The fence carries the change, the worker's object
// is presented as a claim about it, and neither the object nor the word
// "contract" is anywhere the judge could mistake for the deliverable.
func TestAChangedTreeIsWhatTheFenceHolds(t *testing.T) {
	root, record := treeFixture(t)
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":true,"exercised":true}`}}
	evidence := Evidence{Artifacts: record, Workspace: root, Observed: true}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, textualNode(),
		nemotronDeliverable, "", evidence, "worker/model")

	if judgment.Subject != "tree (3 files)" {
		t.Fatalf("the gate did not record what it judged: %q", judgment.Subject)
	}
	prompt := judge.lastPrompt()
	fenced := prompt[strings.Index(prompt, deliverableOpen):strings.Index(prompt, deliverableClose)]
	for _, want := range []string{
		"src/textual/widgets/_log.py",
		"src/textual/widgets/_rich_log.py",
		"tests/test_log.py",
		"is_following_end = True",
	} {
		if !strings.Contains(fenced, want) {
			t.Fatalf("the fence does not hold the change: %q missing from\n%s", want, fenced)
		}
	}
	if strings.Contains(fenced, "contract") {
		t.Fatalf("the worker's own object was fenced as the deliverable:\n%s", fenced)
	}
	claim := prompt[strings.Index(prompt, deliverableClose):]
	if !strings.Contains(claim, "IS NOT THE DELIVERABLE") || !strings.Contains(claim, "contract") {
		t.Fatalf("the worker's account is not below the fence as a claim:\n%s", claim)
	}
}

// A FILE THE RUN ONLY READ IS NOT THE CHANGE IT MADE. The subject stays the
// worker's claim, the fence holds that claim, and the record enum has no file a
// refusal could name.
//
// This is the run of 2026-09-03 in full: a contract that opened "Read CLAUDE.md
// at the repository root before you start", 156 shell calls, not one write, and
// a refusal whose first clause read "The only file this run changed is
// CLAUDE.md". Everything after that clause was true and that clause was not.
func TestAFileTheRunOnlyReadIsNotTheChangeItMade(t *testing.T) {
	root, fileBody := readOnlyFixture(t)
	request := "Read CLAUDE.md at the repository root before you start; its rules bind you"
	workerAnswer := "I read the repository instructions and made no changes."
	evidence := Evidence{
		Named: NamedFiles(request), Workspace: root, Observed: true,
	}
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":true,"exercised":false}`}}
	node := store.Node{ID: "task-1", Provenance: store.Provenance{Intent: request}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, node,
		workerAnswer, "", evidence, "worker/model")

	if judgment.Subject != string(SubjectClaim) {
		t.Fatalf("the gate recorded a file the run only read as its subject: %q", judgment.Subject)
	}
	prompt := judge.lastPrompt()
	opened, closed := strings.Index(prompt, deliverableOpen), strings.Index(prompt, deliverableClose)
	if opened < 0 || closed < opened {
		t.Fatalf("the deliverable fence is missing from the prompt:\n%s", prompt)
	}
	fenced := prompt[opened:closed]
	if !strings.Contains(fenced, workerAnswer) {
		t.Fatalf("the fence does not hold the worker's own answer:\n%s", fenced)
	}
	if strings.Contains(prompt, "CHANGE THIS RUN MADE TO THE TREE") {
		t.Fatalf("a read-only run was presented as a changed tree:\n%s", prompt)
	}
	if strings.Contains(prompt, strings.TrimSpace(fileBody)) {
		t.Fatalf("the file the run only read was printed as its deliverable:\n%s", prompt)
	}

	evidence.completeAgainstTheWorld()
	if evidence.Subject() != SubjectClaim || evidence.SubjectWords() != string(SubjectClaim) {
		t.Fatalf("the swept file changed the subject: %q, %q",
			evidence.Subject(), evidence.SubjectWords())
	}
	if names := evidence.recordNames(); len(names) != 0 {
		t.Fatalf("the verdict shape admits a file the run never wrote: %+v", names)
	}
}

// THE SWEEP STILL ANSWERS THE NAME IT WAS ASKED FOR. Its answer closes only
// absence findings, says in words that the run did not write the file, and
// remains one answer when the world is read twice — which is the half of igel
// s6 that must survive the split.
func TestASweptFileStillAnswersTheNameItWasAskedFor(t *testing.T) {
	root, _ := readOnlyFixture(t)
	evidence := Evidence{
		Named:     NamedFiles("Read CLAUDE.md before you start"),
		Done:      plan.Done{Produces: []string{"CLAUDE.md"}},
		Workspace: root,
		Observed:  true,
	}

	evidence.completeAgainstTheWorld()
	if len(evidence.Swept) != 1 {
		t.Fatalf("the workspace did not answer the named file exactly once: %+v", evidence.Swept)
	}
	first := evidence.Swept[0]
	evidence.completeAgainstTheWorld()
	if len(evidence.Swept) != 1 || evidence.Swept[0] != first {
		t.Fatalf("reading the world twice double-counted its answer: %+v", evidence.Swept)
	}
	if mechanical, missing := MissingProduces(evidence.Done, evidence.producedArtifacts()); missing {
		t.Fatalf("the mechanical gate says a file on disk is missing: %+v", mechanical)
	}
	if missing := evidence.MissingPromised(); len(missing) != 0 {
		t.Fatalf("a file on disk stayed among the missing promises: %+v", missing)
	}
	if closed := AdmitGapArtifact([]string{"CLAUDE.md"}, evidence); closed == "" {
		t.Fatal("a file on disk did not close the absence finding")
	}
	block := evidence.namedBlock()
	if !strings.Contains(block, "a file of that name is there") ||
		!strings.Contains(block, "this run did not write it") {
		t.Fatalf("the named-file block does not distinguish the sweep's answer:\n%s", block)
	}
	if strings.Contains(block, "produced, at") {
		t.Fatalf("a file the run did not write was reported as produced:\n%s", block)
	}
}

// A SWEPT FILE DECIDES NO QUESTION ABOUT WHAT THE RUN CHANGED. A real
// three-file record stays a three-file tree, while its read-only twin has no
// code change, no broken rule, and no wider reading of the project's checks.
func TestASweptFileDoesNotWidenTheChangeTheGateCounts(t *testing.T) {
	root, record := treeFixture(t)
	readmeBody := "This pre-existing file was not part of the change.\n"
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte(readmeBody), 0o600); err != nil {
		t.Fatal(err)
	}
	named := NamedFiles("Read README.md before changing the implementation")
	evidence := Evidence{
		Artifacts: record, Named: named, Workspace: root, Observed: true,
	}
	focusBefore := strings.Join(gateFocus(evidence), "\n")

	evidence.completeAgainstTheWorld()
	if evidence.SubjectWords() != "tree (3 files)" {
		t.Fatalf("the swept file changed the tree's count: %q", evidence.SubjectWords())
	}
	if names := evidence.recordNames(); len(names) != 3 {
		t.Fatalf("the record enum was widened by the swept file: %+v", names)
	}
	tree := evidence.treeBlock(ctxbudget.Budget{})
	if strings.Contains(tree, "README.md") || strings.Contains(tree, strings.TrimSpace(readmeBody)) {
		t.Fatalf("the file this run did not write entered the deliverable fence:\n%s", tree)
	}
	if !codeChanged(evidence) {
		t.Fatal("the run's three recorded files stopped counting as a code change")
	}
	if focusAfter := strings.Join(gateFocus(evidence), "\n"); focusAfter != focusBefore {
		t.Fatalf("the sweep widened the acceptance focus:\nbefore: %s\nafter: %s",
			focusBefore, focusAfter)
	}

	readOnly := Evidence{
		Named: named, Workspace: root, Observed: true,
		Constraints: []plan.Constraint{{Text: "Change no files.", Kind: plan.ConstraintNoWrites}},
	}
	readOnlyFocus := strings.Join(gateFocus(readOnly), "\n")
	readOnly.completeAgainstTheWorld()
	if codeChanged(readOnly) {
		t.Fatal("a file the run only read counted as changed code")
	}
	if held := HoldConstraints(readOnly); len(held) != 0 {
		t.Fatalf("a file the run only read broke a no-writes rule: %+v", held)
	}
	if changed := constraintChanges(readOnly); len(changed) != 0 {
		t.Fatalf("the mechanical constraint check saw a swept file as changed: %+v", changed)
	}
	if focusAfter := strings.Join(gateFocus(readOnly), "\n"); focusAfter != readOnlyFocus {
		t.Fatalf("the read-only sweep widened the acceptance focus:\nbefore: %s\nafter: %s",
			readOnlyFocus, focusAfter)
	}
}

// A FINDING ABOUT THE FENCE IS STRUCTURALLY IMPOSSIBLE OVER A CHANGED TREE.
// The verbatim refusal that shipped three times names no file of the record, so
// it is not a verdict this gate can read: it is asked again, and when the same
// answer comes back the gate FAULTS rather than refusing the delivery over it.
func TestARefusalThatNamesNoFileOfTheRecordIsNotAVerdict(t *testing.T) {
	root, record := treeFixture(t)
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{nemotronVerdict}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, textualNode(),
		nemotronDeliverable, "", Evidence{Artifacts: record, Workspace: root, Observed: true},
		"worker/model")

	if len(judge.sent) != 2 {
		t.Fatalf("the unreadable verdict was not retaken: %d calls", len(judge.sent))
	}
	if judgment.Fault == "" {
		t.Fatalf("a verdict about the fence was accepted as a refusal: %+v", judgment)
	}
	if judgment.Checked || judgment.Gaps != "" {
		t.Fatalf("the fence finding survived as a gap: %+v", judgment)
	}
	// And the re-ask carries the record, so the second answer has somewhere to
	// land: a contract stated without the list it admits is a contract nobody
	// can satisfy.
	if !strings.Contains(judge.lastPrompt(), `"src/textual/widgets/_log.py"`) {
		t.Fatalf("the re-ask did not carry the record it admits:\n%s", judge.lastPrompt())
	}
}

// And the shape a refusal over a changed tree does take: one file of the
// record, quoted behaviour, and a line that opens with the path.
func TestARefusalOverAChangedTreeNamesTheFileFirst(t *testing.T) {
	root, record := treeFixture(t)
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":false,` +
		`"file":"src/textual/widgets/_rich_log.py",` +
		`"gaps":"follow_end is declared and never posts FollowChanged",` +
		`"quote":"a FollowChanged message","exercised":false}`}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, textualNode(),
		nemotronDeliverable, "", Evidence{Artifacts: record, Workspace: root, Observed: true},
		"worker/model")

	if len(judge.sent) != 1 {
		t.Fatalf("a readable verdict was asked again: %d calls", len(judge.sent))
	}
	if !judgment.Checked || judgment.Pass {
		t.Fatalf("the refusal was not taken: %+v", judgment)
	}
	if judgment.File != "src/textual/widgets/_rich_log.py" {
		t.Fatalf("the finding lost the file it is about: %+v", judgment)
	}
	if !strings.HasPrefix(judgment.Gaps, "src/textual/widgets/_rich_log.py — ") {
		t.Fatalf("the line a person reads does not open with the file: %q", judgment.Gaps)
	}
}

// AND AN EMPTY TREE CHANGES NOTHING. A run that left nothing behind is judged
// on its message exactly as it always was, and the mechanical gap — a file the
// plan promised and the disk does not hold — is reached before any model round
// and reads word for word as it did.
func TestAnEmptyRecordKeepsTheClaimSubjectAndTheMechanicalGap(t *testing.T) {
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":true,"exercised":true}`}}
	done := plan.Done{Produces: []string{"report.md"}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, textualNode(),
		"the report is written and verified", "",
		Evidence{Done: done, Workspace: t.TempDir(), Observed: true}, "worker/model")

	if len(judge.sent) != 0 {
		t.Fatalf("a mechanical gap bought a model round: %d calls", len(judge.sent))
	}
	if !judgment.Mechanical || judgment.Pass {
		t.Fatalf("the mechanical gap did not fire: %+v", judgment)
	}
	if judgment.Subject != string(SubjectClaim) {
		t.Fatalf("an empty record was not recorded as a claim: %q", judgment.Subject)
	}
	if !strings.Contains(judgment.Gaps, "report.md") {
		t.Fatalf("the mechanical gap stopped naming the promised file: %q", judgment.Gaps)
	}
}

// A claim-subject delivery is judged on its message, and its refusal needs no
// file: the record holds none, so demanding one would fault every question ever
// answered in prose.
func TestAClaimSubjectRefusalIsUnchanged(t *testing.T) {
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{
		`{"pass":false,"gaps":"no numbers appear anywhere","quote":"a FollowChanged message"}`}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, textualNode(),
		"the parsers differ", "", Evidence{Observed: true}, "worker/model")

	if len(judge.sent) != 1 {
		t.Fatalf("a claim refusal was asked again: %d calls", len(judge.sent))
	}
	if !judgment.Checked || judgment.Gaps != "no numbers appear anywhere" {
		t.Fatalf("the claim refusal did not stand: %+v", judgment)
	}
	if judgment.File != "" {
		t.Fatalf("a claim refusal invented a file: %+v", judgment)
	}
}

// THE FENCE SHAPE IS A REPAIRABLE SHAPE. The object the worker handed over goes
// through the one seam that knows how to say "answer in the shape asked", once,
// and the repair is journaled — so the store shows a structured repair rather
// than three silent re-drives.
func TestADeliverableThatIsADataObjectIsRepairedOnceAndJournaled(t *testing.T) {
	settings := config.Config{Model: "worker/model"}
	worker := &recordingJudge{replies: []string{
		"I added is_following_end to Log and RichLog and posted FollowChanged on the change."}}
	var journaled []shaped.Repair
	ctx := shaped.WithJournal(context.Background(),
		shaped.JournalFunc(func(repair shaped.Repair) { journaled = append(journaled, repair) }))

	reshaped, repaired := ReshapeDelivery(ctx, settings,
		pool.Adopt(settings, worker.Model(), worker), textualNode(), nemotronDeliverable)

	if !repaired || strings.Contains(reshaped, "contract") {
		t.Fatalf("the object was handed on as the deliverable: %q", reshaped)
	}
	if len(worker.sent) != 1 {
		t.Fatalf("the shape was repaired %d times, want exactly one ask", len(worker.sent))
	}
	if len(journaled) != 1 || journaled[0].Kind != shaped.RepairReshaped {
		t.Fatalf("the repair left no record: %+v", journaled)
	}
	if journaled[0].Lane != "delivery" {
		t.Fatalf("the repair was journaled against the wrong pass: %+v", journaled[0])
	}
	// And the ask itself quotes the worker back, because a model told only that
	// its shape was wrong has to guess which part of what it said was the
	// problem.
	if !strings.Contains(worker.lastPrompt(), "contract") {
		t.Fatalf("the re-ask did not quote the answer it is repairing:\n%s", worker.lastPrompt())
	}
}

// A deliverable that is prose is not repaired, and costs nothing. This is every
// delivery in the system but the ones above.
func TestAProseDeliverableIsNeverReshaped(t *testing.T) {
	settings := config.Config{Model: "worker/model"}
	worker := &recordingJudge{replies: []string{"never asked"}}
	answer := "I changed _log.py. The JSON payload {\"a\": 1} is what it now emits."

	reshaped, repaired := ReshapeDelivery(context.Background(), settings,
		pool.Adopt(settings, worker.Model(), worker), textualNode(), answer)

	if repaired || reshaped != answer || len(worker.sent) != 0 {
		t.Fatalf("prose was reshaped: repaired=%v calls=%d", repaired, len(worker.sent))
	}
}

// ── ofetch s12: the excerpt that was judged as the deliverable ───────────────

// ofetchNode is the s12 shape: a request that states behaviours, and a change
// whose largest file is a module the request named.
func circuitNode() store.Node {
	return store.Node{ID: "task-2", Brief: "add a per-origin circuit breaker",
		Provenance: store.Provenance{Intent: "Create a circuit breaker state machine module " +
			"(src/circuit-breaker.ts) that implements the state model, transitions, half-open rules, " +
			"failure accounting, and shared state keyed by origin. " +
			"The circuit must prevent repeated calls to unhealthy origins."}}
}

func circuitPoints() []plan.Point {
	return []plan.Point{
		{Behaviour: "The circuit must prevent repeated calls to unhealthy origins",
			Quote: "The circuit must prevent repeated calls to unhealthy origins"},
		{Behaviour: "Circuit state is keyed by URL origin (not path)",
			Quote: "shared state keyed by origin"},
	}
}

// bigTreeFixture is a change whose one source is far larger than the room, so
// the block has to open it rather than show it whole.
func bigTreeFixture(t *testing.T, bytes int) (root string, record []string) {
	t.Helper()
	root = t.TempDir()
	body := strings.Repeat("export function step() { return 1 }\n", bytes/36+1)
	path := filepath.Join(root, "src", "circuit-breaker.ts")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, []string{path}
}

// A file shown whole says so. A file too large for the room says its full size,
// how much of it is shown, and that the rest exists — because the reading's
// bound reported as a fact about the work is what refused ofetch s12.
func TestNoFileIsEverShownInPart(t *testing.T) {
	root, record := bigTreeFixture(t, 60_000)
	evidence := Evidence{Artifacts: record, Workspace: root, Observed: true}

	oversize := evidence.treeBlock(ctxbudget.Budget{})
	if !strings.Contains(oversize, "EVERY FILE BELOW IS ON DISK, WHOLE, AT THE SIZE STATED BESIDE IT") {
		t.Fatalf("the block does not say the files exist in whole:\n%s", oversize)
	}
	if strings.Contains(oversize, "export function step") {
		t.Fatalf("a file too large for the room was printed in part:\n%s", oversize)
	}
	if !strings.Contains(oversize, "src/circuit-breaker.ts (") {
		t.Fatalf("a file too large to print lost its name and size:\n%s", oversize)
	}

	small, smallRecord := treeFixture(t)
	whole := (Evidence{Artifacts: smallRecord, Workspace: small, Observed: true}).treeBlock(ctxbudget.Budget{})
	if !strings.Contains(whole, "bytes on disk, shown in full") ||
		!strings.Contains(whole, "is_following_end = True") {
		t.Fatalf("a change that fits the room was not shown whole:\n%s", whole)
	}
}

// A REFUSAL IS A BEHAVIOUR THE FILE FAILS, NEVER A CLAIM ABOUT HOW MUCH OF IT
// WAS SHOWN. The s12 verdict named a record file and quoted the request
// verbatim — and what it quoted was the sentence asking for the module to
// exist, not a behaviour the request states. It is not a verdict this gate can
// read, so it is asked again and, unanswered, faults.
func TestARefusalThatQuotesNoStatedBehaviourIsNotAVerdict(t *testing.T) {
	root, record := bigTreeFixture(t, 60_000)
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":false,` +
		`"file":"src/circuit-breaker.ts",` +
		`"gaps":"The deliverable does not include the actual content of the circuit breaker module — the fenced text shows only a truncated excerpt ending mid-sentence.",` +
		`"quote":"Create a circuit breaker state machine module (src/circuit-breaker.ts) that implements the state model, transitions, half-open rules, failure accounting, and shared state keyed by origin.",` +
		`"exercised":false}`}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, circuitNode(), "done", "",
		Evidence{Artifacts: record, Workspace: root, Observed: true, Accept: circuitPoints()},
		"worker/model")

	if len(judge.sent) != 2 {
		t.Fatalf("the verdict about the excerpt was not retaken: %d calls", len(judge.sent))
	}
	if judgment.Fault == "" || judgment.Checked {
		t.Fatalf("a finding about how much of a file was shown was accepted: %+v", judgment)
	}
	// And the judge was shown the behaviours it is held to, so the contract is
	// one it could have satisfied.
	first := judge.sent[0][len(judge.sent[0])-1].Content[0].Text
	if !strings.Contains(first, "The behaviours this request states") ||
		!strings.Contains(first, "The circuit must prevent repeated calls to unhealthy origins") {
		t.Fatalf("the judge was held to a list it was never shown:\n%s", first)
	}
}

// The refusal the same shape admits: a record file, and one of the behaviours
// the request states.
func TestARefusalQuotingAStatedBehaviourStands(t *testing.T) {
	root, record := bigTreeFixture(t, 60_000)
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":false,` +
		`"file":"src/circuit-breaker.ts",` +
		`"gaps":"nothing in it opens the circuit after consecutive failures",` +
		`"quote":"The circuit must prevent repeated calls to unhealthy origins","exercised":false}`}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, circuitNode(), "done", "",
		Evidence{Artifacts: record, Workspace: root, Observed: true, Accept: circuitPoints()},
		"worker/model")

	if len(judge.sent) != 1 {
		t.Fatalf("a readable verdict was asked again: %d calls", len(judge.sent))
	}
	if !judgment.Checked || judgment.Pass || judgment.File != "src/circuit-breaker.ts" {
		t.Fatalf("the refusal did not stand: %+v", judgment)
	}
}

// A request that states no behaviour turns the requirement off rather than
// faulting every refusal: a contract nobody was shown is a contract nobody can
// satisfy.
func TestWithNoStatedBehavioursTheQuoteRequirementIsOff(t *testing.T) {
	root, record := bigTreeFixture(t, 60_000)
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":false,"file":"src/circuit-breaker.ts",` +
		`"gaps":"it never opens the circuit","quote":"prevent repeated calls","exercised":false}`}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, circuitNode(), "done", "",
		Evidence{Artifacts: record, Workspace: root, Observed: true}, "worker/model")

	if len(judge.sent) != 1 || !judgment.Checked {
		t.Fatalf("a refusal was faulted over a list nobody was shown: %d calls, %+v",
			len(judge.sent), judgment)
	}
}

// THE SUBJECT IS STAMPED ON WHATEVER COMES BACK, AT ONE EXIT. A field written
// by each constructor is a field the next constructor forgets, and ofetch s12
// journaled a gate whose subject was empty.
func TestEveryJudgementCarriesWhatItJudged(t *testing.T) {
	root, record := treeFixture(t)
	settings := config.Config{Model: "worker/model"}
	tree := Evidence{Artifacts: record, Workspace: root, Observed: true}

	for name, judged := range map[string]struct {
		reply    string
		evidence Evidence
		want     string
	}{
		"a pass":           {`{"pass":true,"exercised":true}`, tree, "tree (3 files)"},
		"a fault":          {`not an object at all`, tree, "tree (3 files)"},
		"a claim":          {`{"pass":true,"exercised":true}`, Evidence{Observed: true}, "claim"},
		"a mechanical gap": {`{"pass":true}`, Evidence{Done: plan.Done{Produces: []string{"report.md"}}, Workspace: t.TempDir()}, "claim"},
	} {
		judge := &recordingJudge{replies: []string{judged.reply}}
		judgment := JudgeDeliverable(context.Background(), settings,
			pool.Adopt(settings, judge.Model(), judge), nil, textualNode(),
			"x", "", judged.evidence, "worker/model")
		if judgment.Subject != judged.want {
			t.Errorf("%s did not record what it judged: %q, want %q", name, judgment.Subject, judged.want)
		}
	}
}

// ── The two readings that refused each other ────────────────────────────────

// A GOVERNOR MAY REFUSE A ROUND, NEVER A FINDING.
//
// The coverage refusal used to set Overturned here, on the reasoning that
// coverage is a broader measurement than one review's finding. It is not a
// measurement at all: it is a model reading the plan's own Done and the
// workers' own summaries, which is the account being judged. A run answered
// "the job's own reading of what it is judged on found nothing left uncovered,
// so the review's finding is what was wrong" over a behaviour that was dead in
// the delivered tree (2026-09-01, deepseek-v4-flash). So the refusal now says
// which governor spoke, in that governor's words, and leaves the gap Unclosed
// exactly as the rounds cap and the wall do.
func TestACoverageRefusalLeavesTheFindingStanding(t *testing.T) {
	graph := gateStore(t)
	if err := graph.RecordJobGrowth("task-2", store.JobGrowth{Reason: "gap", Lineage: "task-2",
		Round: 1, Allowed: false, Cause: resident.CauseCovered,
		Refused: "everything this job is judged on is already covered"}); err != nil {
		t.Fatal(err)
	}
	node, ok, err := graph.Node("task-2")
	if err != nil || !ok {
		t.Fatalf("node: ok=%v err=%v", ok, err)
	}
	unmet := Judgment{Gaps: "src/circuit-breaker.ts — it never opens the circuit",
		Quote: "persist the feature schema", Citations: []string{"persist the feature schema"},
		File: "src/circuit-breaker.ts", Checked: true,
		Grounds: Grounds{Intent: node.Provenance.Intent}}

	extension := ExtendForGap(context.Background(), graph, node, "done", unmet, nil, 0,
		func(context.Context, string, string) (store.Subtree, error) {
			return store.Subtree{}, nil
		})

	if !extension.Unclosed {
		t.Fatalf("a refused round settled the finding: %+v", extension)
	}
	if !strings.Contains(extension.Refused, "found nothing left to add") {
		t.Fatalf("the person is not told which governor declined: %q", extension.Refused)
	}
	if strings.Contains(extension.Refused, "the review's finding is what was wrong") {
		t.Fatalf("a governor acquitted a finding: %q", extension.Refused)
	}
}

// And a refusal that merely declines to FUND the round leaves the gap standing,
// exactly as it did: rounds, the ceiling, the wall and the rail all say "not
// now" and settle nothing about whether the work landed.
func TestARefusalThatOnlyDeclinesToFundLeavesTheGapStanding(t *testing.T) {
	graph := gateStore(t)
	if err := graph.RecordJobGrowth("task-2", store.JobGrowth{Reason: "gap", Lineage: "task-2",
		Round: 4, Allowed: false, Cause: resident.CauseRounds,
		Refused: "this work has split as many times as splitting helps"}); err != nil {
		t.Fatal(err)
	}
	node, _, err := graph.Node("task-2")
	if err != nil {
		t.Fatal(err)
	}
	unmet := Judgment{Gaps: "src/circuit-breaker.ts — it never opens the circuit",
		Quote: "persist the feature schema", Citations: []string{"persist the feature schema"},
		File: "src/circuit-breaker.ts", Checked: true,
		Grounds: Grounds{Intent: node.Provenance.Intent}}

	extension := ExtendForGap(context.Background(), graph, node, "done", unmet, nil, 0,
		func(context.Context, string, string) (store.Subtree, error) {
			return store.Subtree{}, nil
		})

	if !extension.Unclosed {
		t.Fatalf("a cap was read as the gate being wrong: %+v", extension)
	}
}

// ── textual v4-flash s12: the same defect, twice more ───────────────────────

// Both of that run's gates refused the reading rather than the work:
//
//	"src/textual/widgets/_rich_log.py — The file is truncated — it cuts off
//	 before the implementation of write(expand=True) …"
//	"— The deliverable does not contain the actual content of the files it
//	 claims to have changed. The fenced material is a description of what the
//	 files contain, not the files themselves."
//
// And two structured repairs on lane `gate` say the shape check fired twice and
// the re-ask came back with the same complaint wearing a file and a quote. A
// verdict cannot be argued out of a percept; the percept has to go. A file is
// printed whole or named only, and a refusal has to quote a behaviour.
func TestTheTruncationPerceptCannotBeReached(t *testing.T) {
	root := t.TempDir()
	small := filepath.Join(root, "src", "textual", "widgets", "_log.py")
	if err := os.MkdirAll(filepath.Dir(small), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(small, []byte("class Log:\n    is_following_end = True\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(root, "src", "textual", "widgets", "_rich_log.py")
	if err := os.WriteFile(big, []byte(strings.Repeat("def write(self, expand=False):\n    return self\n", 900)), 0o600); err != nil {
		t.Fatal(err)
	}
	evidence := Evidence{Artifacts: []string{small, big}, Workspace: root, Observed: true}

	block := evidence.treeBlock(ctxbudget.Budget{})
	// The file that fits is entire; the one that does not is a name and a size
	// and nothing a reader could call truncated.
	if !strings.Contains(block, "src/textual/widgets/_log.py — 39 bytes on disk, shown in full") {
		t.Fatalf("the file that fits was not shown whole:\n%s", block)
	}
	if strings.Count(block, "def write(self, expand=False)") != 0 {
		t.Fatalf("the file too large for the room leaked into the page in part:\n%s", block)
	}
	if !strings.Contains(block, "NOTHING BELOW IS AN EXCERPT AND NOTHING ABOVE IS MISSING") {
		t.Fatalf("the page does not say what it is not showing:\n%s", block)
	}

	// And the verdict shape refuses the complaint even so. The quote here is a
	// span of the request that is not one of the behaviours it states, which is
	// what both s12 refusals had.
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":false,` +
		`"file":"src/textual/widgets/_rich_log.py",` +
		`"gaps":"The file is truncated — it cuts off before the implementation of write(expand=True).",` +
		`"quote":"changes to src/textual/widgets/_rich_log.py","exercised":false}`}}
	evidence.Accept = []plan.Point{
		{Behaviour: "RichLog.write(expand=True) preserves full-width justified rendering",
			Quote: "RichLog.write(expand=True) no longer preserves full-width justified rendering"},
	}
	node := textualNode()
	node.Provenance.Intent += " RichLog.write(expand=True) no longer preserves " +
		"full-width justified rendering with current Rich, and the change must " +
		"cover changes to src/textual/widgets/_rich_log.py."

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, node, "done", "",
		evidence, "worker/model")

	if len(judge.sent) != 2 {
		t.Fatalf("the complaint about the reading was not retaken: %d calls", len(judge.sent))
	}
	if judgment.Fault == "" || judgment.Checked {
		t.Fatalf("a complaint about the page was accepted as a finding: %+v", judgment)
	}
}

// textual v4-flash s13 journaled two gates whose subject and quote were both
// right, and nothing in either event said whether the quote had passed the
// checklist enum or there had been no checklist to pass. Those are the
// mechanism working and the mechanism switched off, wearing one event.
func TestTheGateRecordsWhichBehaviourItWasHeldTo(t *testing.T) {
	root, record := treeFixture(t)
	settings := config.Config{Model: "worker/model"}
	node := textualNode()
	node.Provenance.Intent += " Log and RichLog expose is_following_end: bool attribute."
	held := []plan.Point{{Behaviour: "Log and RichLog expose is_following_end",
		Quote: "Log and RichLog expose is_following_end: bool attribute"}}

	for name, judged := range map[string]struct {
		reply  string
		accept []plan.Point
		want   string
	}{
		"a refusal names the behaviour it was built on": {
			`{"pass":false,"file":"tests/test_log.py","gaps":"nothing asserts it",` +
				`"quote":"Log and RichLog expose is_following_end: bool attribute"}`,
			held, "Log and RichLog expose is_following_end: bool attribute"},
		"a pass names the list it was weighed against": {
			`{"pass":true,"exercised":true}`, held, "checklist: 1 behaviour"},
		"and a request that states none says so": {
			`{"pass":true,"exercised":true}`, nil, HeldPointEmpty},
	} {
		// The job's remembered checklist is a package memo, so each case starts
		// from a job that has none — which is the only way the empty case can
		// be the empty case.
		ForgetChecklists()
		judge := &recordingJudge{replies: []string{judged.reply}}
		judgment := JudgeDeliverable(context.Background(), settings,
			pool.Adopt(settings, judge.Model(), judge), nil, node, "done", "",
			Evidence{Artifacts: record, Workspace: root, Observed: true, Accept: judged.accept},
			"worker/model")
		if judgment.HeldPoint != judged.want {
			t.Errorf("%s: held point = %q, want %q", name, judgment.HeldPoint, judged.want)
		}
	}

	// A claim-subject gate has no such list and says nothing rather than
	// claiming an empty one.
	judge := &recordingJudge{replies: []string{`{"pass":true,"exercised":true}`}}
	claim := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, node, "done", "",
		Evidence{Observed: true, Accept: held}, "worker/model")
	if claim.HeldPoint != "" {
		t.Fatalf("a claim gate invented a checklist reading: %q", claim.HeldPoint)
	}
}
