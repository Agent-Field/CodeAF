package session

// The no-progress counter, and what a node that met it leaves behind.
//
// Both tests here are written from ONE REAL RUN. A node was asked for two
// marketing pictures; it explored the workspace, read the site, generated both
// images, looked at each of them, and was regenerating when — fifty-six seconds
// in — it was told "LAND NOW" and landed with "stopped: 6 steps without
// progress". Two files really existed in its worktree at that moment. The
// person got a failed task and no mention of them.
//
// Both halves of that are pinned below: production is progress, and a node that
// is stopped anyway still hands over what it made.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// PAINTING IS WORKING. A node whose whole job is making pictures never calls
// edit or write — it calls generate_image, looks at what came back, and calls it
// again — and a counter that only knew about edit and write read every one of
// those steps as a stall. Four consecutive generations against a threshold of
// three is the exact run that died; it has to land instead.
func TestAPictureMakingNodeIsNotStoppedForLackOfProgress(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	painter := &scriptedMedia{
		base64:    base64.StdEncoding.EncodeToString(pngOfSize(t, 4, 4)),
		mediaType: "image/png",
	}
	arguments, _ := json.Marshal(taskArguments{
		Title: "Paint", Summary: "s", Brief: "paint two of them\n" + taskBriefMark,
		Deliverable: "d", Acceptance: "a", NoProgress: 3, MaxSteps: 30,
	})

	// Four paintings in a row, each at its own path, then the node's own words.
	// Under the old counter the second of these was already a stall.
	var child []step
	for index := 0; index < 4; index++ {
		name := fmt.Sprintf("marketing/sheet-%d.png", index)
		child = append(child, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-paint-"+name, "generate_image",
				`{"prompt":"a drafting-paper spec sheet","path":"`+name+`"}`), nil
		})
	}
	child = append(child, finalText("both sheets are in marketing/"))

	completer := &routedCompleter{
		parent: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("call-task", "propose_task", string(arguments)), nil
			},
			finalText("handed off"),
		},
		child: child,
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskAudit = false
		config.Media = painter
		config.MediaModel = mediaModels(map[string]string{modalityImage: "paint/model"})
	})
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "paint"))

	node := graph.node(1)
	waitDoneNode(t, node)
	notice := node.notice()
	if strings.Contains(notice.Report, "without progress") {
		t.Fatalf("a node that painted four pictures was stopped as spinning: %q", notice.Report)
	}
	if notice.State != TaskDone {
		t.Fatalf("a painting node is %q, want done (report %q)", notice.State, notice.Report)
	}
	// And every picture it made is named to the person, off the arguments of the
	// calls that made them.
	for index := 0; index < 4; index++ {
		want := fmt.Sprintf("marketing/sheet-%d.png", index)
		if !containsString(notice.Changed, want) {
			t.Fatalf("changed = %v, want it to name %s", notice.Changed, want)
		}
	}
}

// A NODE THAT IS STOPPED STILL HANDS OVER WHAT IT MADE. The threshold cancels
// the work; it does not condemn the files. Before this, nothing but a clean
// merge ever committed — so a stopped node was landed with a report naming a
// branch that had nothing on it, its real work sitting untracked in a directory
// nobody mentioned.
//
// The merge itself is deliberately NOT attempted: only checked work reaches the
// person's branch. What changes is that the branch the report names now holds
// the work, so `git merge` on it is a real offer.
func TestStoppedWorkIsCommittedToTheBranchItsReportNames(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "aaaa1111aaaa1111", 1, "make the sheets")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	// What the node made: one file at a path a call named, and one under a name
	// it chose for itself — a picture generated with no path argument, which no
	// reader of the arguments could ever have listed.
	writeFile(t, filepath.Join(tree.dir, "marketing", "linkedin.png"), "png\n")
	writeFile(t, filepath.Join(tree.dir, "marketing", "20260819-blueprint.png"), "png\n")
	// And the harness's own droppings, which are not the node's work.
	writeFile(t, filepath.Join(tree.dir, aforgeDroppings, "jobs", "1.log"), "building\n")

	merge, changed := keptWork(tree, "make the sheets", []string{"marketing/linkedin.png"})
	if merge != mergeAborted {
		t.Fatalf("merge = %q, want %q — a kept branch is still not a merged one", merge, mergeAborted)
	}
	for _, want := range []string{"marketing/linkedin.png", "marketing/20260819-blueprint.png"} {
		if !containsString(changed, want) {
			t.Fatalf("changed = %v, want it to name %s", changed, want)
		}
	}
	for _, unwanted := range changed {
		if strings.HasPrefix(unwanted, aforgeDroppings) {
			t.Fatalf("changed = %v, want the harness's own droppings left out", changed)
		}
	}

	// THE CLAIM IS NOW TRUE: the branch the person is pointed at holds the work.
	listed, err := git(repo, "ls-tree", "-r", "--name-only", tree.branch)
	if err != nil {
		t.Fatalf("git ls-tree: %v\n%s", err, listed)
	}
	for _, want := range []string{"marketing/linkedin.png", "marketing/20260819-blueprint.png"} {
		if !strings.Contains(listed, want) {
			t.Fatalf("branch %s holds:\n%s\nwant %s on it", tree.branch, listed, want)
		}
	}
	if strings.Contains(listed, aforgeDroppings) {
		t.Fatalf("branch %s carries the harness's own droppings:\n%s", tree.branch, listed)
	}

	// And the person's own branch is untouched: nothing merged.
	if _, err := os.Stat(filepath.Join(repo, "marketing")); err == nil {
		t.Fatal("stopped work merged into the person's tree; only checked work may")
	}

	// The sentence the model reads says where it is.
	note := taskNote(TaskNotice{
		ID: 1, Title: "make the sheets", State: TaskFailed,
		Report: "stopped: 6 steps without progress", Changed: changed,
		Branch: tree.branch, Merge: mergeAborted,
	}, "")
	if !strings.Contains(note, tree.branch) || !strings.Contains(note, "merge that branch to take the work") {
		t.Fatalf("the note does not offer the work back:\n%s", note)
	}
	// A node that left nothing is not sent after work that does not exist.
	empty := taskNote(TaskNotice{
		ID: 2, Title: "read it", State: TaskFailed, Branch: "task/read", Merge: mergeAborted,
	}, "")
	if strings.Contains(empty, "merge that branch to take the work") {
		t.Fatalf("an empty branch was offered as a deliverable:\n%s", empty)
	}
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
