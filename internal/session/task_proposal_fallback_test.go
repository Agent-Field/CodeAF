package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A HAND-OFF THE RUN ENGINE COULD NOT START SAYS SO, AND STARTS NOTHING ELSE.
//
// The run road's store will not open — a directory stands where the store file
// goes. The receipt the conversation reads must not be the run road's receipt
// word for word, and the work must not quietly become a node of the older
// engine's tree: an approved hand-off under the bash belt is a run or it is
// nothing, and the receipt says which, with the run road's own reason
// ([runDidNotStart]).
func TestAnApprovedHandoffTheRunEngineCouldNotStartSaysSo(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("never reached")
	registerBeltRunEngine(t, double)
	dir := t.TempDir()
	// The store's own name, taken by a directory: the run road cannot open it.
	if err := os.MkdirAll(filepath.Join(dir, planStoreFilename, "in-the-way"), 0o755); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, beltRunCompleter{text: "never reached"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	agent.graph().run = func(*TaskNode) {}
	before := agent.graph().seq

	answer, failed, err := approveBeltProposal(t, agent, beltProposalArgs("Change the fallback road", "the focused proof passes"))
	if err != nil {
		t.Fatalf("propose_task errored the turn: %v", err)
	}
	if !failed || !strings.Contains(answer, "did not start") {
		t.Fatalf("the receipt hides that the run engine could not start the task: failed=%v\n%s", failed, answer)
	}
	if agent.graph().node(before+1) != nil {
		t.Fatal("the hand-off the run engine could not start became a node of the older tree")
	}
}
