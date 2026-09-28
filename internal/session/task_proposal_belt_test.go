package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

func beltProposalArgs(title, acceptance string, depends ...uint64) json.RawMessage {
	body, _ := json.Marshal(taskArguments{
		Title: title, Summary: "the proposed work", Brief: "change the proposed behavior",
		Deliverable: "the changed behavior", Acceptance: acceptance, DependsOn: depends,
	})
	return body
}

func approveBeltProposal(t *testing.T, agent *Agent, args json.RawMessage) (string, bool, error) {
	t.Helper()
	staged := agent.stageTask(context.Background(), args)
	proposal, ok := staged.(*stagedProposal)
	if !ok {
		return staged.Commit(context.Background())
	}
	agent.ResolveTask(proposal.id, TaskAnswer{Approved: true})
	return proposal.Commit(context.Background())
}

func TestApprovedProposalBashBeltStartsRunWithoutSessionNode(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("the proposal ran")
	registerBeltRunEngine(t, double)
	dir := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "the proposal ran"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	agent.graph().run = func(*TaskNode) {}

	answer, failed, err := approveBeltProposal(t, agent, beltProposalArgs("Change the proposal road", "the focused proof passes"))
	if err != nil || failed {
		t.Fatalf("propose_task: failed=%v err=%v answer=%q", failed, err, answer)
	}
	store := beltRunStoreAt(t, dir)
	defer store.Close()
	root := store.Task(store.RootID())
	if root == nil {
		t.Fatal("the approved proposal created no plandb root")
	}
	id, _ := strconv.ParseUint(root.ID, 10, 64)
	if agent.graph().node(id) != nil {
		t.Fatalf("approved proposal %s also admitted a running session-tree node", root.ID)
	}
	if !strings.Contains(root.Description, "the focused proof passes") {
		t.Fatalf("root description lost acceptance: %q", root.Description)
	}
	endBeltRun(t, agent, double)
}

func TestApprovedProposalBashBeltJoinsLiveRunWithPlanDependencies(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("the run did the work")
	registerBeltRunEngine(t, double)
	dir := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "the run did the work"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	root, _, _, err := agent.StartTask(context.Background(), "open the run", false)
	if err != nil {
		t.Fatal(err)
	}
	<-double.entered
	upstream, _, _, err := agent.StartTask(context.Background(), "prepare the input", false)
	if err != nil {
		t.Fatal(err)
	}
	answer, failed, err := approveBeltProposal(t, agent, beltProposalArgs("Consume the input", "the output uses the prepared input", upstream))
	if err != nil || failed {
		t.Fatalf("propose_task: failed=%v err=%v answer=%q", failed, err, answer)
	}
	if !strings.Contains(answer, "It joined the work already underway and shares its copy.") {
		t.Fatalf("joined hand-off receipt = %q, want the same-ground join said plainly", answer)
	}

	store := beltRunStoreAt(t, dir)
	defer store.Close()
	var proposed *plandb.Task
	for _, task := range store.Tasks() {
		if task.Title == "Consume the input" {
			proposed = task
			break
		}
	}
	if proposed == nil {
		t.Fatal("approved proposal did not join the live plan")
	}
	if proposed.ParentID != strconv.FormatUint(root, 10) {
		t.Fatalf("parent=%q want live root %d", proposed.ParentID, root)
	}
	if len(proposed.Dependencies) != 1 || proposed.Dependencies[0].TaskID != strconv.FormatUint(upstream, 10) {
		t.Fatalf("dependencies=%v want %d", proposed.Dependencies, upstream)
	}
	id, _ := strconv.ParseUint(proposed.ID, 10, 64)
	if agent.graph().node(id) != nil {
		t.Fatalf("proposal %s also admitted a session-tree node", proposed.ID)
	}
	endBeltRun(t, agent, double)
}

func TestApprovedProposalWithoutBeltKeepsSessionTreeRoad(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "node")
	double := newBeltRunDouble("must not run")
	registerBeltRunEngine(t, double)
	dir := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "unused"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	agent.graph().run = func(*TaskNode) {}
	before := agent.graph().seq
	answer, failed, err := approveBeltProposal(t, agent, beltProposalArgs("Keep the old road", "the session node runs"))
	if err != nil || failed {
		t.Fatalf("propose_task: failed=%v err=%v answer=%q", failed, err, answer)
	}
	if agent.graph().node(before+1) == nil {
		t.Fatal("belt-off proposal admitted no session-tree node")
	}
	if _, err := os.Stat(filepath.Join(dir, planStoreFilename)); err == nil {
		t.Fatal("belt-off proposal seeded a plan store")
	}
	if double.didRun() {
		t.Fatal("belt-off proposal reached the run engine")
	}
}

// THE RUN OUTLIVES THE TURN THAT LAUNCHED IT. The hand-off commits under the
// turn's own context, which the turn cancels on its way out; a run driven under
// that context would stop the moment the model finished its sentence.
func TestApprovedProposalsRunSurvivesTheEndOfTheTurnThatLaunchedIt(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("the proposal ran")
	registerBeltRunEngine(t, double)
	dir := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "the proposal ran"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	agent.graph().run = func(*TaskNode) {}

	turn, endTurn := context.WithCancel(context.Background())
	staged := agent.stageTask(turn, beltProposalArgs("Outlive the turn", "the run is still going"))
	proposal, ok := staged.(*stagedProposal)
	if !ok {
		t.Fatalf("the proposal was not staged: %T", staged)
	}
	agent.ResolveTask(proposal.id, TaskAnswer{Approved: true})
	if answer, failed, err := proposal.Commit(turn); err != nil || failed {
		t.Fatalf("propose_task: failed=%v err=%v answer=%q", failed, err, answer)
	}
	<-double.entered
	endTurn()

	double.mu.Lock()
	running := double.ctx
	double.mu.Unlock()
	if running == nil {
		t.Fatal("the run engine was never started")
	}
	if err := running.Err(); err != nil {
		t.Fatalf("the run's context ended with the turn that launched it: %v", err)
	}
	endBeltRun(t, agent, double)
}

func TestApprovedProposalBashBeltRefusesDifferentGroundInsteadOfSessionTree(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("the run did the work")
	registerBeltRunEngine(t, double)
	dir := t.TempDir()
	conversation := newTestRepo(t)
	alternate := newTestRepo(t)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "the run did the work"}, func(config *Config) {
		config.Workspace = conversation
		config.Place = Place{Dir: dir}
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	if _, _, _, err := agent.StartTask(context.Background(), "open the run", false); err != nil {
		t.Fatal(err)
	}
	<-double.entered
	args, _ := json.Marshal(taskArguments{
		Title: "Work elsewhere", Summary: "the proposed work", Brief: "change the proposed behavior",
		Deliverable: "the changed behavior", Acceptance: "the focused proof passes", Ground: alternate,
	})
	staged := agent.stageTask(context.Background(), args)
	proposal, ok := staged.(*stagedProposal)
	if !ok {
		t.Fatalf("the proposal was not staged: %T", staged)
	}
	agent.ResolveTask(proposal.id, TaskAnswer{Approved: true})
	answer, failed, err := proposal.Commit(context.Background())
	if err != nil || !failed || !strings.Contains(answer, "share one copy of one folder") || !strings.Contains(answer, "Propose it again when that work has ended") {
		t.Fatalf("different-ground hand-off: failed=%v err=%v answer=%q", failed, err, answer)
	}
	if node := agent.graph().node(proposal.id); node != nil {
		t.Fatalf("different-ground hand-off silently became session-tree task %+v", node)
	}
	endBeltRun(t, agent, double)
}
