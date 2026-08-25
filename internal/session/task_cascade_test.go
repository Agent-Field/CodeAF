package session

// THE CASCADE: WHERE A CHEAP CREW BUYS THE CAREFUL MODEL, AND WHERE IT DOES NOT.
//
// One escalation exists in this build (repair_role.go): work a checker sent back
// is handed to [roleRepair], which sits on the careful tier. Four things have to
// hold for that to be an economy rather than a leak, and each of them is a test
// here:
//
//   - it happens on the REPAIR ROUND and nowhere else — the first attempt is the
//     cheap crew's, exactly as it was;
//   - a model somebody NAMED for the work wins over it, because the cascade is a
//     default and not an override;
//   - an all-flash crew floors onto its own model with no branch and no error;
//   - and the escalation is WRITTEN DOWN when it happens, so the money can be
//     found afterwards, and not written down when it did not.
//
// The fifth is what the escalated model is actually asked: the gaps, the frozen
// acceptance, and the change as it stands — never the whole repository again.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// carefulTier is what these tests set the careful class to. It is deliberately
// nothing like `test/model`, the model [newTestAgent] runs the session on, so
// "the repair moved" and "the repair stayed" can never be read the same way.
const carefulTier = "test/careful-model"

// ── the role ────────────────────────────────────────────────────────────────

// THE TIER IS THE DECISION, so it is asserted rather than assumed. A repair on
// the cheap tier is the behaviour this whole lane replaced — the same hands that
// just came back short, asked again — and a repair on the mastermind tier would
// be paying a thinking model's price for many turns of ordinary editing, which
// is the split [roles.TierMastermind] exists to keep.
func TestTheRepairRoleSitsOnTheCarefulTier(t *testing.T) {
	tier, ok := roles.TierOf(roleRepair)
	if !ok {
		t.Fatalf("%q is not a registered role, so it resolves to nothing", roleRepair)
	}
	if tier != roles.TierHigh {
		t.Fatalf("%q resolves on %q, want the careful tier", roleRepair, tier)
	}
	// A ROW A PERSON READS. The settings sheet lists every registered role, and a
	// role with no line under its name is a call somebody pays for and cannot
	// read (internal/roles' Describe states the emptiness law about it).
	if strings.TrimSpace(roles.Describe(roleRepair)) == "" {
		t.Fatalf("%q has no description, so its settings row says only its own name", roleRepair)
	}
}

// ── which model one round resolves to ───────────────────────────────────────

// cascadeAgent is a session with a crew configured and one node in its graph,
// which is the whole of what [Agent.repairModel] reads.
func cascadeAgent(t *testing.T, tiers map[string]string, spec taskSpec) (*Agent, *TaskNode) {
	t.Helper()
	agent, _ := newTestAgent(t, &routedCompleter{}, func(config *Config) {
		if tiers != nil {
			config.RolesSource = tierSettings(tiers)
		}
	})
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	node := &TaskNode{graph: graph, id: 1, spec: spec, state: TaskRunning}
	graph.nodes[1] = node
	return agent, node
}

// A REFUTED NODE'S NEXT WORKER IS THE CAREFUL ONE. The node ran on the cheap
// model, the crew's careful class is set, and the round that closes the gaps
// resolves there — which is the whole purchase.
func TestARepairRoundResolvesThroughTheRepairRolesTier(t *testing.T) {
	agent, node := cascadeAgent(t,
		map[string]string{roles.TierKey(roles.TierHigh): carefulTier},
		taskSpec{title: "the greeting", model: "vendor/flash"})

	if got := agent.repairModel(node); got != carefulTier {
		t.Fatalf("the repair round resolves to %q, want the careful tier's %q", got, carefulTier)
	}
	// AND THE ROLE IS THE ROAD IT TOOK, not the tier read directly: a pin on the
	// role alone moves the round and nothing else in the crew.
	pinned, _ := cascadeAgent(t,
		map[string]string{
			roles.TierKey(roles.TierHigh): carefulTier,
			roles.PinKey(roleRepair):      "vendor/mender",
		},
		taskSpec{title: "the greeting", model: "vendor/flash"})
	if got := pinned.repairModel(node); got != "vendor/mender" {
		t.Fatalf("a pin on %q resolved to %q", roleRepair, got)
	}
}

// A MODEL SOMEBODY NAMED WINS. The cascade is the harness's answer to "whose
// hands should close these gaps" when nobody said; a default that overrode an
// explicit pick would be this package deciding it knows better than the person
// watching the work.
func TestAModelSomebodyNamedIsNotEscalated(t *testing.T) {
	agent, node := cascadeAgent(t,
		map[string]string{roles.TierKey(roles.TierHigh): carefulTier},
		taskSpec{title: "the sweep", modelWord: "opus 5", model: "anthropic/claude-opus-5"})

	if got := agent.repairModel(node); got != "anthropic/claude-opus-5" {
		t.Fatalf("the repair round resolves to %q, want the model that was named", got)
	}
}

// AND A PICK MADE INSIDE THE NODE'S OWN ROOM IS A PICK. [TaskNode.retarget] is
// the person choosing for one node, and the cascade has to be able to see it —
// a retarget that moved only the id would leave their choice looking like a
// default the harness was free to overrule on the next round.
func TestAModelPickedInTheRoomIsNotEscalatedEither(t *testing.T) {
	agent, node := cascadeAgent(t,
		map[string]string{roles.TierKey(roles.TierHigh): carefulTier},
		taskSpec{title: "the sweep", model: "vendor/flash"})

	if got := agent.repairModel(node); got != carefulTier {
		t.Fatalf("before anybody picked, the repair resolves to %q", got)
	}
	node.retarget("openai/gpt-5")
	if !node.modelPicked() {
		t.Fatal("a retarget left the node looking like one that named no model")
	}
	if got := agent.repairModel(node); got != "openai/gpt-5" {
		t.Fatalf("the repair round resolves to %q, want the model picked in the room", got)
	}
}

// AN ALL-FLASH CREW REPAIRS ON FLASH, and it is the ladder's own floor that says
// so rather than a branch anybody wrote: a careful class set to the model the
// work is already on, and a crew with no classes set at all, are the same
// answer.
func TestAnAllFlashCrewRepairsOnTheModelItIsAlreadyOn(t *testing.T) {
	for name, tiers := range map[string]map[string]string{
		"careful is the same model": {roles.TierKey(roles.TierHigh): "vendor/flash"},
		"nothing is configured":     nil,
	} {
		t.Run(name, func(t *testing.T) {
			agent, node := cascadeAgent(t, tiers, taskSpec{title: "the greeting", model: "vendor/flash"})
			if got := agent.repairModel(node); got != "vendor/flash" {
				t.Fatalf("the repair round resolves to %q, want the model the work is on", got)
			}
		})
	}
}

// AND A NODE THAT NAMED NOTHING AT ALL FLOORS ON THE CONVERSATION'S MODEL rather
// than on nothing: a scripted graph and a checkpoint written by an older build
// both leave `spec.model` empty, and a repair that resolved to "" would be a
// request with no model on it.
func TestARepairFloorsOnTheSessionsModelWhenTheNodeNamesNone(t *testing.T) {
	agent, node := cascadeAgent(t, nil, taskSpec{title: "the greeting"})
	if got := agent.repairModel(node); got != "test/model" {
		t.Fatalf("the repair round resolves to %q, want the session's own model", got)
	}
}

// ── what the project's record says about the money ──────────────────────────

// THE ESCALATION IS WRITTEN DOWN ONLY WHERE IT HAPPENED. A row saying "repaired
// on flash" for a node that was on flash the whole time is a fact nobody can
// spend, and the bench reading this file is asking one question: where did the
// extra money go.
func TestTheRecordNamesTheRepairsModelOnlyWhenItMoved(t *testing.T) {
	entry := func(on, ran string) TaskIndexEntry {
		t.Helper()
		graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
		node := &TaskNode{graph: graph, id: 4, state: TaskDone,
			spec: taskSpec{title: "the greeting", model: on}}
		node.repairedOn(ran)
		graph.mu.Lock()
		defer graph.mu.Unlock()
		return node.indexEntryLocked("aaaa1111aaaa1111")
	}

	moved := entry("vendor/flash", carefulTier)
	if moved.RepairedOn != carefulTier {
		t.Fatalf("repairedOn = %q, want the model the round actually ran on", moved.RepairedOn)
	}
	if moved.Model != "vendor/flash" {
		t.Fatalf("model = %q, want the model the node itself ran on", moved.Model)
	}
	// A ROUND THAT FLOORED SAYS NOTHING, and neither does a node nobody sent back.
	for name, got := range map[string]TaskIndexEntry{
		"the ladder floored": entry("vendor/flash", "vendor/flash"),
		"nothing was sent back": func() TaskIndexEntry {
			graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
			node := &TaskNode{graph: graph, id: 5, state: TaskDone,
				spec: taskSpec{title: "the greeting", model: "vendor/flash"}}
			graph.mu.Lock()
			defer graph.mu.Unlock()
			return node.indexEntryLocked("aaaa1111aaaa1111")
		}(),
	} {
		if got.RepairedOn != "" {
			t.Fatalf("%s: repairedOn = %q, want nothing", name, got.RepairedOn)
		}
	}

	// AND IT IS ADDITIVE: the field is absent from a row that has none, so every
	// row this project ever wrote still reads back as one.
	raw, err := json.Marshal(entry("vendor/flash", "vendor/flash"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "repairedOn") {
		t.Fatalf("a row with no escalation carries the field: %s", raw)
	}
	path := filepath.Join(t.TempDir(), taskIndexName)
	appendTaskIndex(path, moved)
	rows := ReadTaskIndex(path)
	if len(rows) != 1 || rows[0].RepairedOn != carefulTier {
		t.Fatalf("the row came back as %+v, want the escalation on it", rows)
	}
}

// ── what the escalated model is actually asked ──────────────────────────────

// THE EXPENSIVE MODEL READS THE DIFF, NOT THE WORLD.
//
// A worker handed a brief goes and finds out what is in the repository, which is
// right on a fresh task and pure waste on a repair round — the tree it is
// standing in was filled by the last worker. Paid for at the careful tier, that
// waste is the most expensive way there is to learn something the harness
// already knew. So the round opens on the change: the files, the shape of the
// diff, and where the whole of it can be read in one command — with the frozen
// acceptance and the gaps verbatim, which is what it is being asked about.
func TestTheRepairInstructionCarriesTheGapsTheAcceptanceAndTheChange(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "greet.go"), "package greet\n\nfunc Greet() string { return \"hi\" }\n")
	mustGit(t, repo, "add", "-A")

	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	node := &TaskNode{graph: graph, id: 1, spec: taskSpec{
		title:      "the greeting",
		request:    "add a greeting",
		brief:      "write greet.go and a test for it",
		acceptance: "go test ./... passes and there is a test for Greet",
	}}
	node.brief = node.spec.brief
	tree := taskTree{dir: repo, root: repo, branch: "task/greeting"}
	verdict := auditVerdict{answered: true,
		evidence: []string{"go test ./... reports no test files", "greet_test.go does not exist"}}

	instruction := repairInstruction(node, tree, verdict, []string{"greet.go"})

	for what, want := range map[string]string{
		"the gaps, verbatim":            "go test ./... reports no test files",
		"the second gap":                "greet_test.go does not exist",
		"the review's own heading":      repairHeading,
		"the rule that the work stands": repairStands,
		"the frozen acceptance":         "there is a test for Greet",
		"the person's own words":        "add a greeting",
		"what is already in the tree":   repairSawHeading,
		"the files the work wrote":      "greet.go",
		"where the whole diff is":       repairDiffPointer,
	} {
		if !strings.Contains(instruction, want) {
			t.Errorf("the repair round is never told %s (%q)", what, want)
		}
	}
	// THE SHAPE OF THE CHANGE, off the index the checker already staged.
	if !strings.Contains(instruction, "1 file changed") && !strings.Contains(instruction, "greet.go |") {
		t.Errorf("the repair round was given no `git diff --cached --stat`:\n%s", instruction)
	}
	// ORIENTATION COMES BEFORE THE FINDING, and the rule that bounds the finding
	// comes last: the two sentences about THIS round sit together at the end.
	if at, gaps := strings.Index(instruction, repairSawHeading), strings.Index(instruction, repairHeading); at > gaps {
		t.Errorf("the change is described after the gaps, at %d and %d", at, gaps)
	}

	// A WORKSPACE THAT IS NOT A REPOSITORY SAYS SO rather than pointing at a diff
	// that cannot be read — the auditor's own arrangement, one layer down.
	inplace := repairInstruction(node, taskTree{dir: repo}, verdict, []string{"greet.go"})
	if !strings.Contains(inplace, repairNoDiff) {
		t.Errorf("an in-place tree was still told to read a diff:\n%s", inplace)
	}
	if strings.Contains(inplace, repairDiffPointer) {
		t.Errorf("an in-place tree was pointed at `git diff --cached`")
	}
}

// ── end to end ──────────────────────────────────────────────────────────────

// repairWatch is [routedCompleter] with the MODEL of every request kept beside
// it, split by whether the request carried a repair round's instruction.
//
// It reads the model off the options the way internal/orchestrate's own model
// tests do — replaying them onto a bare [ai.Request] — because that is the only
// place the resolution is observable from outside the agent that made it.
type repairWatch struct {
	*routedCompleter
	mu      sync.Mutex
	first   []string
	repairs []string
}

func (w *repairWatch) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	brief, repairing := false, false
	for _, message := range messages {
		text := messageText(message)
		if strings.Contains(text, taskBriefMark) {
			brief = true
		}
		if strings.Contains(text, repairHeading) {
			repairing = true
		}
	}
	if brief {
		w.mu.Lock()
		if repairing {
			w.repairs = append(w.repairs, request.Model)
		} else {
			w.first = append(w.first, request.Model)
		}
		w.mu.Unlock()
	}
	return w.routedCompleter.CompleteWithMessages(ctx, messages, options...)
}

func (w *repairWatch) ran() ([]string, []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.first...), append([]string(nil), w.repairs...)
}

// THE WHOLE CASCADE, OVER A REAL REPOSITORY. The first worker writes the package
// and forgets the test, the checker runs `go test` and says so, and the round
// that closes the gap runs on the careful class — in the same worktree, on the
// same node, with the escalation on the row this project keeps.
func TestARefutedNodesRepairRoundRunsOnTheCarefulModel(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	watch := &repairWatch{routedCompleter: &routedCompleter{
		parent: []step{
			proposeCall("Add the greeting", "write greet.go and a test for it"),
			finalText("handed off"),
		},
		child: nodeLane(8, func(repairing, wrote bool) *ai.Response {
			switch {
			case repairing && !wrote:
				return writeResponse("call-test", "greet_test.go",
					"package greet\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) {\n\tif Greet() != \"hi\" {\n\t\tt.Fatal(\"no greeting\")\n\t}\n}\n")
			case repairing:
				return pricedResponse("Added greet_test.go, which covers the greeting.", 0.02)
			case !wrote:
				return writeResponse("call-src", "greet.go", "package greet\n\nfunc Greet() string { return \"hi\" }\n")
			default:
				return pricedResponse("Wrote greet.go with the greeting.", 0.01)
			}
		}),
		audit: []step{
			bashCall("call-verify", "go test ./..."),
			verdictFromEvidence("ok  \t",
				"VERIFIED — go test ./... ok",
				"REFUTED — go test ./... reports no test files: the acceptance asks for a test and there is none"),
			bashCall("call-verify-again", "go test ./..."),
			verdictFromEvidence("ok  \t",
				"VERIFIED — go test ./... ok · the greeting test runs",
				"REFUTED — go test ./... still reports no test files"),
		},
	}}

	agent, _ := newTestAgent(t, watch, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskRepairRounds = 1
		config.RolesSource = tierSettings(map[string]string{roles.TierKey(roles.TierHigh): carefulTier})
	})
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "add a greeting"))

	node := graph.node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	waitDoneNode(t, node)
	notice := node.notice()
	if notice.State != TaskDone {
		t.Fatalf("state = %q, report = %q — the repaired work did not land", notice.State, notice.Report)
	}

	first, repairs := watch.ran()
	if len(first) == 0 {
		t.Fatal("the first worker was never asked anything")
	}
	for _, model := range first {
		if model != "test/model" {
			t.Fatalf("the FIRST attempt ran on %q — the cascade is only ever bought after a failure", model)
		}
	}
	if len(repairs) == 0 {
		t.Fatal("no repair round ran, so nothing was escalated")
	}
	for _, model := range repairs {
		if model != carefulTier {
			t.Fatalf("the repair round ran on %q, want the careful class %q", model, carefulTier)
		}
	}

	// THE NODE DID NOT MOVE. A repair round is one worker among several a node
	// takes, and the row still names the model the work was admitted on.
	if notice.Model == carefulTier {
		t.Fatalf("the node's own row now names the repair's model %q", notice.Model)
	}
	// AND THE PROJECT'S RECORD SAYS WHERE THE EXTRA MONEY WENT.
	graph.mu.Lock()
	entry := node.indexEntryLocked("aaaa1111aaaa1111")
	graph.mu.Unlock()
	if entry.RepairedOn != carefulTier {
		t.Fatalf("repairedOn = %q, want the model the escalation bought", entry.RepairedOn)
	}
	// One node, one branch, one merge — the escalation changed none of that.
	if count := admitted(graph); count != 1 {
		t.Fatalf("%d nodes in the graph, want 1", count)
	}
	if _, err := os.Stat(filepath.Join(repo, "greet_test.go")); err != nil {
		t.Fatalf("the repaired half is not on the person's branch: %v", err)
	}
}
