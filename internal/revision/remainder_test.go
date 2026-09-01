package revision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// THE REMAINDER JUDGE WAS SHOWN ALMOST NOTHING.
//
// It received the brief and the worker's final paragraph — and the prompt told
// it, in as many words, that exhaustion is not evidence of incompleteness. So a
// leaf cut off mid-`sh` with `undefined: logf` in its build was judged finished
// on its own closing sentence, "All 722 tests pass. Let me verify the dry-run
// tests specifically:", and the node was ✓ two seconds later. Every fact that
// would have contradicted it was already in the record and none of it was read.
func TestTheRemainderJudgeIsShownWhatItIsJudging(t *testing.T) {
	graph := gateStore(t)
	if err := graph.RecordLeafExhausted("task-2", store.LeafExhausted{
		Attempt: 1, Bound: "budget", Turns: 33, Meter: "cost",
		Reached: 199131, Allowance: 176834, Unit: "tokens of billed work",
		Reason: "it was still working when it ran out of its tokens — 33 turns in",
	}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordTranscript("task-2", "worker/model", []store.TranscriptEntry{
		{Turn: 32, Kind: store.TranscriptToolCall, Tool: "bash", CallID: "c1",
			Text: `{"command":"go build ./..."}`},
		{Turn: 32, Kind: store.TranscriptToolResult, Tool: "bash", CallID: "c1",
			Text: "internal/exec/runner.go:41:2: undefined: logf", Failed: true},
	}); err != nil {
		t.Fatal(err)
	}
	patch := filepath.Join(t.TempDir(), "change.diff")
	if err := os.WriteFile(patch, []byte("--- a/runner.go\n+++ b/runner.go\n+\tlogf(\"hi\")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	node, ok, err := graph.Node("task-2")
	if err != nil || !ok {
		t.Fatalf("node: ok=%v err=%v", ok, err)
	}
	evidence := Evidence{Patch: patch, Verification: verify.Reading{
		Taken: true, AfterTaken: true,
		After: verify.Result{Entrypoint: verify.Entrypoint{Command: "go test ./..."},
			Exit: 2, Reported: []string{"TestRunnerLogs"}, Failing: []string{"TestRunnerLogs"}},
	}}

	subject := remainderSubject(graph, node, evidence)
	for _, needed := range []struct{ what, needle string }{
		{"that the worker was cut off", "still working when it ran out"},
		{"how far it got", "33 turns"},
		{"the turns it actually took", "go build ./..."},
		{"the change it made", "undefined: logf"},
		{"what the project's own checks said", "go test ./..."},
	} {
		if !strings.Contains(subject, needed.needle) {
			t.Fatalf("the judge is not shown %s (%q):\n%s", needed.what, needed.needle, subject)
		}
	}
}

// A FACT THAT CANNOT BE READ RENDERS NOTHING. A judge told "the change:"
// followed by an empty block reads it as a run that changed nothing, which is
// the direction that acquits — so a run with no record simply says less.
func TestTheRemainderJudgeIsShownNoEmptyHeadings(t *testing.T) {
	graph := gateStore(t)
	node, ok, err := graph.Node("task-2")
	if err != nil || !ok {
		t.Fatalf("node: ok=%v err=%v", ok, err)
	}
	if subject := remainderSubject(graph, node, Evidence{}); strings.TrimSpace(subject) != "" {
		t.Fatalf("a run with nothing in the record was described anyway:\n%s", subject)
	}
	if subject := remainderSubject(nil, node, Evidence{}); strings.TrimSpace(subject) != "" {
		t.Fatalf("a judgement with no store was described anyway:\n%s", subject)
	}
}

// AND THE PROMPT NO LONGER TELLS THE JUDGE TO DISCOUNT WHAT IT IS SHOWN. The
// sentence "exhaustion is not evidence of incompleteness" was doing the opposite
// of its intent: it was written to stop a finished leaf being continued, and
// what it actually did was instruct a judge to ignore the one measured fact
// about the run it was judging.
func TestTheRemainderPromptWeighsTheEvidenceOverTheAccount(t *testing.T) {
	if strings.Contains(remainderPrompt, "exhaustion is not evidence of incompleteness") {
		t.Fatal("the prompt still tells the judge to discount the one measured fact it has")
	}
	if !strings.Contains(remainderPrompt, "own account of itself is not evidence") {
		t.Fatalf("the prompt does not say whose words are not evidence:\n%s", remainderPrompt)
	}
	if strings.TrimSpace(RemainderVerify) == "" {
		t.Fatal("a cut leaf whose judge found nothing left is aimed at nothing")
	}
}
