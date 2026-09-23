package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A HAND-OFF THE RUN ENGINE COULD NOT START SAYS WHICH ENGINE TOOK IT, AND WHY.
//
// The run road's store will not open — a directory stands where the store file
// goes — so the approved task falls back to the older engine. The work still
// starts, but the receipt the conversation reads must not be the run road's
// receipt word for word: it names the engine the work is on and the run road's
// own reason, so the chat never describes a run that does not exist.
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

	answer, failed, err := approveBeltProposal(t, agent, beltProposalArgs("Change the fallback road", "the focused proof passes"))
	if err != nil || failed {
		t.Fatalf("propose_task: failed=%v err=%v answer=%q", failed, err, answer)
	}
	if !strings.Contains(answer, "It runs on the older task engine, because the run engine could not start it:") {
		t.Fatalf("the receipt hides that the run engine could not start the task:\n%s", answer)
	}
}
