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
	close(double.release)
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
	close(double.release)
}

func TestApprovedProposalWithoutBeltKeepsSessionTreeRoad(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "")
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
