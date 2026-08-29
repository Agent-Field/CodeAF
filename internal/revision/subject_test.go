package revision

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
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
