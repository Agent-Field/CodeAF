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
	}, "", TaskSettleAsk)
	if !strings.Contains(note, tree.branch) || !strings.Contains(note, "merge that branch to take the work") {
		t.Fatalf("the note does not offer the work back:\n%s", note)
	}
	// A node that left nothing is not sent after work that does not exist.
	empty := taskNote(TaskNotice{
		ID: 2, Title: "read it", State: TaskFailed, Branch: "task/read", Merge: mergeAborted,
	}, "", TaskSettleAsk)
	if strings.Contains(empty, "merge that branch to take the work") {
		t.Fatalf("an empty branch was offered as a deliverable:\n%s", empty)
	}
}

// ── the landing, and what is still owed to the work ─────────────────────────
//
// These three are written from a SECOND real run. A node was asked for six
// stories; it wrote all six, then spent six steps re-reading them to satisfy
// itself, and the counter stopped it — correctly, because the same target twice
// IS the spin. Its landing turn said "The six files are already written…
// Done — Chapter 1… The files are the deliverable", the six files were on disk,
// and the person got ✗ failed and a kept branch next to a report saying it had
// finished. The threshold had never been a statement about the deliverable, and
// the harness was reading it as one ([Agent.landStopped]).

// spinLane answers the node's own lane by what is already in its context: it
// produces its deliverable if it has one, then aims at the SAME target over and
// over — the exact spin the counter exists to catch — and answers the LAND NOW
// turn with its own account of the work.
//
// It is written this way rather than positionally for [nodeLane]'s reason: the
// harness's own title call speaks on this lane too, and a script that counted
// turns would be asserting the order those happen to fall in.
//
// THE SPIN IS FINITE. The counter cancels the run, and the cancel is a message
// to a real model — this fake never reads one — so the lane stops calling tools
// of its own accord once the threshold has had every step it needs. What follows
// is the LAND NOW turn, which is the whole reason these tests exist.
const spinReads = 5

func spinLane(turns int, produce func() *ai.Response, landed string) []step {
	steps := make([]step, turns)
	for index := range steps {
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if len(messages) > 0 && strings.Contains(messageText(messages[0]), "name this session") {
				return textResponse("the spinning task"), nil
			}
			calls := 0
			for _, message := range messages {
				if strings.Contains(messageText(message), "LAND NOW") {
					return textResponse(landed), nil
				}
				if message.Role == "tool" {
					calls++
				}
			}
			if produce != nil {
				if calls == 0 {
					return produce(), nil
				}
				calls--
			}
			if calls >= spinReads {
				return textResponse("still looking it over."), nil
			}
			// One target, read again and again: no new file, no new dirt, nothing
			// learned. The first of these is still a fresh target and still counts
			// as progress; every one after it is the stall.
			return toolResponse(fmt.Sprintf("call-look-%d", calls), "read", `{"path":"go.mod"}`), nil
		}
	}
	return steps
}

// spinningTask is the propose call for a node that will be stopped for spinning:
// a threshold low enough to fire inside a scripted run, and a real acceptance,
// because the acceptance is the whole thing a landed node is now judged against.
func spinningTask(t *testing.T, acceptance string) step {
	t.Helper()
	arguments, err := json.Marshal(taskArguments{
		Title: "Write the greeting", Summary: "two lines the person reads",
		Brief:       "write greet.go, then check it\n" + taskBriefMark,
		Deliverable: "greet.go at the root of the worktree",
		Acceptance:  acceptance,
		NoProgress:  3, MaxSteps: 30,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse("call-task", "propose_task", string(arguments)), nil
	}
}

// A LANDED NODE THAT ACTUALLY FINISHED ITS WORK IS DONE. It writes the file the
// acceptance asks for, spins on re-reading it until the counter cancels the run,
// and lands. The check it gets is the one an ordinary finishing node gets — and
// because the work holds, the node settles done, its branch comes home, and the
// counter's sentence is nowhere in what the person reads. The report belongs to
// the deliverable, not to the threshold that interrupted the re-reading.
func TestALandedNodeWhoseWorkHoldsSettlesDoneAndMerges(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := &routedCompleter{
		parent: []step{spinningTask(t, "greet.go exists at the root of the worktree"), finalText("handed off")},
		child: spinLane(16, func() *ai.Response {
			return writeResponse("call-src", "greet.go", "package greet\n\nfunc Greet() string { return \"hi\" }\n")
		}, "greet.go is already written and checked. I'll stop looping and deliver.\nDone — greet.go holds the greeting."),
		// The checker does its real job: it looks at the staged tree and answers on
		// what it saw, so VERIFIED here means the file really is there.
		audit: []step{
			bashCall("call-look", "git status --porcelain"),
			verdictFromEvidence("greet.go",
				"VERIFIED — git status --porcelain shows greet.go staged",
				"REFUTED — git status --porcelain shows no greet.go"),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "write the greeting"))

	node := graph.node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	waitDoneNode(t, node)
	notice := node.notice()

	if notice.State != TaskDone {
		t.Fatalf("state = %q, want done — the work was there (report %q)", notice.State, notice.Report)
	}
	if notice.Merge != mergeMerged {
		t.Fatalf("merge = %q, want merged — checked work comes home", notice.Merge)
	}
	if _, err := os.Stat(filepath.Join(repo, "greet.go")); err != nil {
		t.Fatalf("greet.go is not on the person's branch: %v", err)
	}
	// THE COUNTER IS NOT THE NEWS. Nothing a person reads mentions the threshold
	// on work that was checked and merged.
	if strings.Contains(notice.Report, "without progress") || strings.Contains(notice.Report, "stopped") {
		t.Fatalf("report = %q, want the threshold's sentence gone from work that holds", notice.Report)
	}
	if !strings.HasPrefix(notice.Report, "greet.go is already written") {
		t.Fatalf("report = %q, want the work's own account first", notice.Report)
	}
	if !containsString(notice.Changed, "greet.go") {
		t.Fatalf("changed = %v, want it to name greet.go", notice.Changed)
	}
}

// AND A LANDED NODE WHOSE WORK DOES NOT HOLD KEEPS TODAY'S ENDING. It wrote a
// real file and it is not the file that was asked for; the checker says so; the
// node stays stopped, the threshold leads the report, the branch is kept with
// the work committed on it, and nothing reaches the person's tree.
func TestALandedNodeWhoseWorkDoesNotHoldStaysStoppedWithItsBranchKept(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := &routedCompleter{
		parent: []step{spinningTask(t, "greet.go exists at the root of the worktree"), finalText("handed off")},
		child: spinLane(16, func() *ai.Response {
			return writeResponse("call-notes", "notes.md", "# what I was thinking about\n")
		}, "I wrote up my notes. Done, I think."),
		audit: []step{
			bashCall("call-look", "git status --porcelain"),
			verdictFromEvidence("greet.go",
				"VERIFIED — git status --porcelain shows greet.go staged",
				"REFUTED — git status --porcelain shows notes.md and no greet.go"),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "write the greeting"))

	node := graph.node(1)
	waitDoneNode(t, node)
	notice := node.notice()

	if notice.State != TaskFailed {
		t.Fatalf("state = %q, want failed — the work is not what was asked for (report %q)", notice.State, notice.Report)
	}
	if !strings.HasPrefix(notice.Report, "stopped: 3 steps without progress") {
		t.Fatalf("report = %q, want the threshold that fired to lead", notice.Report)
	}
	if notice.Merge != mergeAborted {
		t.Fatalf("merge = %q, want the branch kept rather than merged", notice.Merge)
	}
	if _, err := os.Stat(filepath.Join(repo, "notes.md")); err == nil {
		t.Fatal("work that did not hold reached the person's tree")
	}
	// The branch the report names holds what the node made, exactly as before.
	listed, err := git(repo, "ls-tree", "-r", "--name-only", notice.Branch)
	if err != nil {
		t.Fatalf("git ls-tree: %v\n%s", err, listed)
	}
	if !strings.Contains(listed, "notes.md") {
		t.Fatalf("branch %s holds:\n%s\nwant notes.md kept on it", notice.Branch, listed)
	}
	// AND THE ENDING IS HONEST ON PURPOSE RATHER THAN BY ACCIDENT: somebody did
	// look at this work before it was left on its branch.
	if len(completer.auditAsked()) == 0 {
		t.Fatal("the landed work was never checked")
	}
}

// AND THE ORDINARY STUCK NODE IS UNTOUCHED BY ANY OF THIS. It read the same
// thing four times, made nothing, and said so. It lands exactly where it always
// landed: stopped, failed, its branch kept and empty, nothing merged.
func TestAStuckNodeWithNothingToShowLandsStoppedAsBefore(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := &routedCompleter{
		parent: []step{spinningTask(t, "greet.go exists at the root of the worktree"), finalText("handed off")},
		child:  spinLane(16, nil, "I could not work out what to write and have nothing to hand over."),
		audit: []step{
			bashCall("call-look", "git status --porcelain"),
			verdictFromEvidence("greet.go",
				"VERIFIED — git status --porcelain shows greet.go staged",
				"REFUTED — the worktree is empty: nothing was written"),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "write the greeting"))

	node := graph.node(1)
	waitDoneNode(t, node)
	notice := node.notice()

	if notice.State != TaskFailed {
		t.Fatalf("state = %q, want failed (report %q)", notice.State, notice.Report)
	}
	if !strings.HasPrefix(notice.Report, "stopped: 3 steps without progress") {
		t.Fatalf("report = %q, want the threshold that fired to lead", notice.Report)
	}
	if notice.Merge != mergeAborted {
		t.Fatalf("merge = %q, want nothing merged", notice.Merge)
	}
	if len(notice.Changed) != 0 {
		t.Fatalf("changed = %v, want nothing named for a node that made nothing", notice.Changed)
	}
	if _, err := os.Stat(filepath.Join(repo, "greet.go")); err == nil {
		t.Fatal("a node that wrote nothing put something on the person's branch")
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
