//go:build e2e

// TWO CONTRACTS, END TO END, AGAINST A REAL MODEL.
//
// WHAT THIS LANE PROVES. A worker is handed one document — its contract — and it
// does what the document says. Both issues below are the same failure read from
// two sides: the contract said something about a place or a command that was
// TRUE OF SOMEBODY ELSE, and the worker obeyed it anyway.
//
//   - #566, a worktree task and the addresses in its contract. A task that is
//     given a private copy is stood up in `<session>/trees/<id>` and, before the
//     fix, handed a WHAT TO PRODUCE naming absolute paths in the person's LIVE
//     checkout. A worker that follows the address it was given reads a directory
//     that is still moving under it, and its first write at that address is
//     refused by the ground guard as `is outside your copy`. After the fix the
//     contract's addresses resolve inside the task's own copy, the write lands
//     there, and nothing says `is outside your copy`.
//
//   - #569, a family-wide check and every part's done-condition. When a task
//     divides, the same broad check command copied into EVERY child's
//     done-condition orders the same expensive run several times over — each in a
//     tree that does not yet hold its siblings' files, so only the last of them
//     could be right. After the fix `divide_work` does not admit a division whose
//     siblings each carry the same command: it comes back as a plain refusal
//     beginning `not split:` naming that command, and the worker carries on.
//
// EVERY ASSERTION IS ON DISK OR ON THE SESSION'S OWN RECORDS. The node rows in
// the graph's checkpoint, the files in the task's private copy, the division
// records and tool results in the workers' own journals, the person's git
// history. Not one reads the model's prose, because a model saying it worked in
// its own copy is exactly the claim these tests exist to check.
//
// WHAT IT COSTS AND HOW IT IS PINNED. Every call rides deepseek/deepseek-v4-flash
// through [pinEveryTextModel], and that pinning is CHECKED rather than assumed:
// each run reads the machine's own usage ledger back through [familyRun.report]
// and fails if any other model answered.
//
//	go test -tags e2e -count=1 -timeout 40m -v \
//	  -run 'TestAWorktreeTaskFollowsItsContractInsideItsOwnCopy|TestOneWideCheckIsNotOrderedByEveryPart' \
//	  ./internal/e2e/
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

const (
	// contractWall is how long one attempt at either scenario may take from the
	// moment the person's message is submitted. A cheap model changing one line,
	// or writing three eighty-word sections, is minutes; twelve is the far edge
	// of that and still an answer rather than a hang. It is larger than
	// [familyWall] by the length of the chat turn that proposes the work, which
	// [runFamily]'s typed door does not pay for.
	contractWall = 12 * time.Minute
	// contractAttempts is the ask and one retry, for [familyAttempts]'s reason: a
	// cheap model may keep small work in its own hands, or write a contract with
	// no address in it, and neither is a fault in the road. What is not allowed
	// is passing quietly, so a scenario that never reached the shape it is about
	// fails with everything the run recorded.
	contractAttempts = 2
)

// ── #566: the addresses in a worktree task's contract ───────────────────────

// theWidget is the one file the #566 fixture is about, spelled once so that the
// source path, the copy's path and the address the worker is handed are all
// derived from the same name rather than typed three times. It is the same name
// internal/session's own replication uses, in a suffix no toolchain will try to
// build.
const theWidget = "internal/widget.md"

// theWidgetBefore and theWidgetAfter are the two words the whole scenario turns
// on: what the person's checkout holds when the work starts, and what the work
// is for. They are one word each so that "did this file change" is a comparison
// no whitespace can blur.
const (
	theWidgetBefore = "old\n"
	theWidgetAfter  = "new"
)

// outsideYourCopy is the ground guard's refusal, spelled as taskoutside.go
// spells it. It is the sentence #566 is measured by: a worker following its own
// contract must never be handed it.
const outsideYourCopy = "is outside your copy"

// TestAWorktreeTaskFollowsItsContractInsideItsOwnCopy is #566 against a real
// model: the person names their repository and asks for one line changed, the
// chat proposes the task with the file's ABSOLUTE address in the contract, and
// the worker — which is given a private copy of that repository — is asked to
// produce it.
//
// THE ADDRESS IS THE WHOLE SCENARIO, so the person's message spells it and asks
// for it to be carried into the brief, the deliverable and the done-condition.
// That is not the test putting its thumb on the scale: it is the ordinary shape
// of a proposal written by a parent standing in the source checkout, which is
// what prompts/system.md asks for and what the issue was reported against.
func TestAWorktreeTaskFollowsItsContractInsideItsOwnCopy(t *testing.T) {
	w := newWorld(t)
	pinEveryTextModel(t)

	var last *contractAttempt
	for attempt := 1; attempt <= contractAttempts; attempt++ {
		one := runWorktreeContract(t, w, attempt)
		if one.root.Mode == string(session.TaskModeWorktree) && one.root.Worktree != "" {
			assertTheWorkerStayedInItsCopy(t, one)
			return
		}
		t.Logf("attempt %d: no worktree task with a copy of its own came of this ask (mode %q, copy %q)",
			attempt, one.root.Mode, one.root.Worktree)
		last = one
	}
	t.Fatalf("no worktree task with a private copy came of this ask in %d attempts.\nthe nodes were:\n%s",
		contractAttempts, last.run.nodeLog())
}

// contractAttempt is one run of the #566 scenario: the family record the engine
// wrote, the node that did the work, and what the two directories held while it
// was standing in one of them.
type contractAttempt struct {
	run    *familyRun
	root   taskRow
	copies *copyWatch
	source *copyWatch
}

// runWorktreeContract puts the ask once, on a fresh repository and a fresh
// conversation, and waits for the whole graph to come to rest.
//
// IT GOES THROUGH A CHAT TURN AND NOT THROUGH [session.Agent.StartTask], which
// is the one place this file departs from [runFamily] and it is deliberate. A
// task a person types has no deliverable — their words are the whole of it
// (task_person.go) — so the WHAT TO PRODUCE section #566 is about would not
// exist. The proposal door is the door the issue was reported against.
func runWorktreeContract(t *testing.T, w *world, attempt int) *contractAttempt {
	t.Helper()
	ground := newWidgetGround(t)
	desk := filepath.Join(t.TempDir(), "desk")
	if err := os.MkdirAll(desk, 0o755); err != nil {
		t.Fatalf("make the conversation's own folder: %v", err)
	}
	place := w.place(w.projectBucket(desk), desk)
	agent := w.openAt(desk, place, familyConfig(w))
	if _, err := agent.ReferPlace(ground, session.PlaceSaid); err != nil {
		t.Fatalf("refer %s: %v", ground, err)
	}

	// THE TWO DIRECTORIES ARE WATCHED FROM BEFORE THE WORK STARTS, because a
	// worktree that has landed is REMOVED: by the time the graph is at rest
	// there is nothing at `trees/<id>` to read, so what the copy held has to be
	// taken while the worker is still standing in it.
	copies := watchFile(place.Trees(), filepath.Join("*", filepath.FromSlash(theWidget)))
	source := watchFile(ground, filepath.FromSlash(theWidget))
	defer copies.stop()
	defer source.stop()

	started := time.Now()
	said := w.say(agent, widgetAsk(ground), answerYes)
	t.Logf("the chat called: %v", said.names())

	run := &familyRun{t: t, w: w, agent: agent, place: place, ground: ground}
	ctx, cancel := context.WithTimeout(context.Background(), contractWall)
	defer cancel()
	run.root = awaitRootNode(ctx, t, place)
	run.timedOut = !run.waitForRest(ctx)
	run.wall = time.Since(started)
	run.rows = readTaskRows(t, place.Tasks())
	run.divisions = readDivisions(t, place)
	run.usd, run.models = ledgerSince(t, started)
	if run.root != 0 {
		run.report(attempt)
	}
	if run.timedOut {
		t.Fatalf("attempt %d did not come to rest inside %s; the nodes are:\n%s", attempt, contractWall, run.nodeLog())
	}
	if run.root == 0 {
		t.Fatalf("attempt %d: the chat proposed no task at all; it called %v and said %q",
			attempt, said.names(), shorten(said.Reply, 400))
	}
	root, _ := run.node(run.root)
	return &contractAttempt{run: run, root: root, copies: copies, source: source}
}

// widgetAsk is the person's message. It names the repository and the file's full
// address, and asks for one line changed and nothing else.
//
// EVERY SENTENCE IN IT IS SOMETHING A WORKER MAY ACT ON, and that had to be
// learned. It ended "do not make the edit yourself and do not read the file
// first: propose the task and stop" — an instruction meant for the conversation
// — and the person's words are carried VERBATIM into the worker's own contract
// as the thing that wins where anything disagrees with it (task_brief.go's
// [briefAskRule]). The worker read its own orders as "stop", did one read and
// finished with nothing. A person's message in this file is written as work.
func widgetAsk(ground string) string {
	return fmt.Sprintf(`The repository at %s is what this work is about. It holds %s, whose only
line is "old", and it has to say "new" instead. Change nothing else.

Start ONE task with propose_task to do it, with the task's ground set to %s. In the
task's brief, in its deliverable and in its done-condition, name the file by its full
address

    %s

spelled exactly like that every time, because that is the address this work is about.`,
		ground, theWidget, ground, filepath.Join(ground, filepath.FromSlash(theWidget)))
}

// newWidgetGround is the person's checkout: one commit holding the one file the
// work is about, and an identity of their own so the harness's commits are told
// apart from theirs.
func newWidgetGround(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(filepath.Join(dir, "internal"), 0o755); err != nil {
		t.Fatalf("make the person's repository: %v", err)
	}
	gitAt(t, dir, "init", "--quiet")
	gitAt(t, dir, "config", "user.name", "the person")
	gitAt(t, dir, "config", "user.email", "person@localhost")
	gitAt(t, dir, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(theWidget)), []byte(theWidgetBefore), 0o644); err != nil {
		t.Fatalf("seed the person's repository: %v", err)
	}
	gitAt(t, dir, "add", "-A")
	gitAt(t, dir, "commit", "--quiet", "-m", "the widget")
	return dir
}

// assertTheWorkerStayedInItsCopy is #566 read off the disk and the records.
func assertTheWorkerStayedInItsCopy(t *testing.T, one *contractAttempt) {
	t.Helper()
	run, root := one.run, one.root

	// (1) THE RECORD SAYS WHAT THE COPY IS OF AND WHERE IT IS. Binding a
	// contract's addresses moves the worker's paths, never the node's
	// provenance: the ground stays the person's repository and the working
	// directory stays this session's own `trees/<id>`.
	if !samePath(t, root.Ground, run.ground) {
		t.Errorf("the task stood on %q, not on the person's repository %q", root.Ground, run.ground)
	}
	if !withinTrees(t, run.place, root.Worktree) {
		t.Errorf("the task worked at %q, which is not under this session's trees/ (%s)", root.Worktree, run.place.Trees())
	}
	if want := filepath.Join(run.place.Trees(), fmt.Sprint(root.ID)); !samePath(t, root.Worktree, want) {
		t.Errorf("the task's copy is %q, not the session's own %q", root.Worktree, want)
	}
	t.Logf("  the node's record: mode=%s ground=%s copy=%s merge=%s changed=%v",
		root.Mode, root.Ground, root.Worktree, root.Merge, root.Changed)

	// (2) THE WORKER WAS NEVER REFUSED. This is the sentence the issue is named
	// for, and its ABSENCE is the whole of what the fix buys: a worker following
	// the address it was given is not told the address is somebody else's.
	said := run.journals()
	if strings.Contains(said, outsideYourCopy) {
		t.Errorf("a worker following its own contract was refused %q — the contract pointed outside the copy the task was given (#566).\n%s",
			outsideYourCopy, quoteAround(said, outsideYourCopy, 3))
	} else {
		t.Logf("  no worker in this family was ever told %q", outsideYourCopy)
	}

	// (3) THE ADDRESS IT WAS ACTUALLY HANDED. The contract is read out of the
	// worker's OWN opening message — the document it obeyed — rather than out of
	// the spec the chat proposed, because binding happens on the way to the
	// worker and the spec is not where a worker reads.
	//
	// A CONTRACT THAT NAMES NO ADDRESS IS A FINDING AND NOT A RED. That is a
	// fact about what one cheap model wrote on one day; the laws above and below
	// are about the road, and they hold either way.
	opening := openingMessage(t, run.place, root.ID)
	switch handed := addressUnder(opening, briefProduceHeading, theWidget); {
	case handed == "":
		t.Logf("  FINDING: the worker's WHAT TO PRODUCE names no address for %s at all, so this attempt says nothing about which directory it was pointed at", theWidget)
	case withinTrees(t, run.place, handed):
		t.Logf("  the worker's WHAT TO PRODUCE names %s, inside the copy it was given (#566)", handed)
	default:
		t.Errorf("the worker's WHAT TO PRODUCE names %q, which is not under this session's trees/ — the contract was not bound to the copy the task was given, whose working directory is %q (#566)",
			handed, root.Worktree)
	}

	// (4) THE WORK LANDED INSIDE THE COPY. Read off the disk while the worker
	// was still standing in it, at the one path a worktree task may write.
	inTheCopy, at := one.copies.lastSeen()
	switch {
	case at == "":
		t.Errorf("%s was never seen inside any of this session's trees/ while the work ran; the worker wrote nowhere the road can land from (#566)", theWidget)
	case !strings.Contains(inTheCopy, theWidgetAfter):
		t.Errorf("the copy's %s at %s last held %q, which does not say %q — the work did not land in the task's own directory (#566)",
			theWidget, at, shorten(inTheCopy, 200), theWidgetAfter)
	default:
		t.Logf("  the copy's %s at %s held %q while the worker stood in it", theWidget, at, strings.TrimSpace(inTheCopy))
	}
	// AND WHERE THE WORK DID NOT LAND, WHAT THE WORKER ACTUALLY DID. A failure
	// saying only that a file is missing sends whoever reads it to a directory
	// that has already been removed; the traffic is the one record of the
	// addresses the worker reached for and what it was handed back.
	if at == "" || !strings.Contains(inTheCopy, theWidgetAfter) {
		for _, id := range []uint64{root.ID} {
			t.Logf("  what task #%d actually did:\n%s", id, toolTraffic(journalOf(t, run.place, id)))
		}
	}

	// (5) AND THE PERSON'S REPOSITORY WAS CHANGED ONLY BY THE ORDINARY LANDING.
	//
	// TWO READINGS, BECAUSE ONE OF THEM ALONE PASSES ON THE BUG. Their branch was
	// made by one commit and must have moved exactly once since — [familyRun]'s
	// own reflog reading — and their working file must have taken exactly one
	// step, from what they had to what the work made. A worker writing into their
	// checkout mid-run shows up as a third reading in the watcher's sequence, and
	// a landing that never happened shows up as one.
	branch := gitAt(t, run.ground, "rev-parse", "--abbrev-ref", "HEAD")
	moved := lines(gitTry(run.ground, "reflog", "show", "--format=%gs", branch))
	t.Logf("  the person's branch %s moved %d time(s): %v", branch, len(moved), moved)
	if root.Merge == "merged" && len(moved) != 2 {
		t.Errorf("the person's branch %s moved %d time(s) — its own commit and then %d more; one landing is one move onto their branch: %v",
			branch, len(moved), len(moved)-1, moved)
	}
	if dirty := strings.TrimSpace(gitTry(run.ground, "status", "--porcelain")); dirty != "" {
		t.Errorf("the person's repository is dirty after the run, so something wrote into it outside the landing:\n%s", dirty)
	}
	steps := one.source.steps()
	t.Logf("  the person's own %s took %d step(s) during the run: %v", theWidget, len(steps)-1, quoteAll(steps))
	if len(steps) > 0 && steps[0] != theWidgetBefore {
		t.Errorf("the person's %s started the run holding %q, not %q; the watcher missed the beginning and the reading below is not trustworthy",
			theWidget, steps[0], theWidgetBefore)
	}
	if len(steps) > 2 {
		t.Errorf("the person's %s changed %d times while the work ran: %v — a worktree task changes their checkout once, at the landing (#566)",
			theWidget, len(steps)-1, quoteAll(steps))
	}
	if root.Merge == "merged" {
		if body := readGroundFile(t, run.ground, theWidget); !strings.Contains(body, theWidgetAfter) {
			t.Errorf("the person's %s holds %q after a landed task; the work never came home", theWidget, shorten(body, 200))
		}
	} else {
		t.Logf("  FINDING: the task came home as %q rather than merged, so nothing here says what reached the person's history", root.Merge)
	}
}

// briefProduceHeading is the contract's own heading for the thing to make, as
// task_brief.go spells it. It is repeated here rather than reached for because
// it is unexported there and this lane reads the disk.
const briefProduceHeading = "WHAT TO PRODUCE"

// briefDoneWhenHeading is the section after it, which is where the WHAT TO
// PRODUCE section stops.
const briefDoneWhenHeading = "DONE WHEN"

// addressUnder is the ONE address a worker was actually handed for a file: the
// path naming it under the given heading of its opening message.
//
// IT READS THAT SECTION AND NOTHING ELSE ON PURPOSE. The person's own words are
// carried verbatim above it and the brief restates them, so a search over the
// whole document would find whichever spelling came first rather than the one
// the worker is contractually pointed at.
func addressUnder(text, heading, name string) string {
	start := strings.Index(text, heading)
	if start < 0 {
		return ""
	}
	body := text[start+len(heading):]
	if end := strings.Index(body, briefDoneWhenHeading); end >= 0 {
		body = body[:end]
	}
	return regexp.MustCompile(`[^\s"'` + "`" + `]*` + regexp.QuoteMeta(name)).FindString(body)
}

// ── #569: one wide check, ordered by every part ─────────────────────────────

// theWideCheck is the whole-project check the #569 ask names, spelled as one
// clause of a done-condition because that is where the rule reads it.
//
// ITS FIRST WORD IS A PROGRAM THE CHECKER'S SHELL WILL FIND, which is what makes
// the division road read the clause as an ORDER TO RUN something rather than as
// a condition somebody reads (task_divide_scope.go's [orderedChecks]). And it is
// genuinely family-wide: run inside any one part's worktree it answers about a
// tree that does not hold its siblings' sections, so only the run the parent
// makes once every part's work is home could be right.
//
// AND IT NAMES NO FILE AT ALL, WHICH IS NOT AN AESTHETIC CHOICE. It was
// `wc -w a.md b.md c.md` first, and a real-model run on the unfixed tree never
// reached this rule: OWNERSHIP IS READ OFF THE DONE-CONDITION (#281,
// task_divide_scope.go's [scopeCollisions]), so a check naming all three
// deliverables in all three done-conditions is three parts claiming one file,
// and every division was turned down for scope before the shared-check rule was
// ever asked. Any dotted or slashed word in a done-condition is a claim —
// [looksLikePath] and [groundHolds] together — so a family-wide check that is to
// be ABOUT the family rather than about its files must name none of them.
const theWideCheck = "ls -1 lists every section of the report"

// theWideCommand is the program half of that clause — what a run of it actually
// looks like in a shell call, which is how the parent's own run of it is read
// back out of its journal. It is separate from the clause because a
// done-condition is a sentence and a receipt is a command line, and matching one
// against the other would find nothing.
const theWideCommand = "ls -1"

// divisionRefusedSharedCheck is the decision the division road writes when it
// turns a division down for this rule (task_divide.go). Spelled again here for
// [taskRow]'s reason: it is unexported there and this lane reads the record.
const divisionRefusedSharedCheck = "refused:shared-check"

// divisionRepairedSharedCheck is the SECOND firing of that rule on one node: the
// division is admitted with the family-wide command taken off every part and
// carried by the parent instead (task_divide.go). Spelled again here for
// [divisionRefusedSharedCheck]'s reason.
const divisionRepairedSharedCheck = "repaired:shared-check"

// TestOneWideCheckIsNotOrderedByEveryPart is #569 against a real model: a task
// wide enough to divide over three disjoint files, whose ask names one
// whole-project check and asks for it in every part's done-condition.
//
// WHAT IS ASSERTED IS THE LAW, AND WHAT IS REPORTED IS THE MODEL. The law is
// that no more than one admitted child may carry that command. What the worker
// then does with the refusal — re-ask with a check per slice, or carry the work
// alone — is a fact about one cheap model on one day, so it is measured, logged
// in full and asserted about nowhere. That is the same line [theWorkerSaidWhatItOwns]
// draws in families_e2e_test.go, and it is drawn for the same reason: a law
// hostage to a model's manners is not a law.
func TestOneWideCheckIsNotOrderedByEveryPart(t *testing.T) {
	w := newWorld(t)
	pinEveryTextModel(t)
	// THE WIDTH FLOOR IS OFF, for TestFamilies' reason: internal/splitgate
	// refuses a division whose evidence names fewer than six separate items, and
	// three sections is deliberately below that floor. Whether handing work out
	// PAYS is a different question from what a part is finished against.
	t.Setenv("AFORGE_SPLITGATE", "0")

	run := familyWithParts(t, w, newFolderGround, wideCheckAsk, nil, theWideCheckShapeWasReached,
		"no division carrying one whole-project check in more than one part ever reached the road")

	nodes := readContractNodes(t, run.place.Tasks())
	carriers := childrenOrdering(nodes, run.root, theWideCheck)
	refusals := run.divisionsDeciding(divisionRefusedSharedCheck)

	// THE LAW. One command standing in the done-condition of two or more
	// siblings is the whole of the waste #569 names: three concurrent runs of a
	// family-wide check plus the parent's own afterwards, and only the last of
	// them able to be right.
	if len(carriers) > 1 {
		var said []string
		for _, one := range carriers {
			said = append(said, fmt.Sprintf("    part #%d %q is done when: %s", one.ID, one.Title, shorten(oneLine(one.Acceptance), 400)))
		}
		t.Errorf("%d of this family's children were each ordered to run %q — the same family-wide check, bought once per part, each in a tree that does not hold its siblings' files (#569):\n%s",
			len(carriers), theWideCheck, strings.Join(said, "\n"))
	}

	// AND THE RUN IS NOT ALLOWED TO PASS VACUOUSLY. A model that never wrote the
	// command into more than one part produced a family this issue says nothing
	// about, and calling that green would be calling a road untested.
	if len(carriers) <= 1 && len(refusals) == 0 {
		t.Fatalf("no division in this family ever put %q into more than one part, so this run says nothing about #569.\nthe divisions on record were:\n%s",
			theWideCheck, run.divisionLog())
	}

	// THE REFUSAL, WHERE ONE FIRED. The record carries the decision and the
	// commands; the sentence the worker was handed is what names the command it
	// can act on, and it is in that worker's own journal as an ordinary tool
	// result.
	said := run.journals()
	for _, one := range refusals {
		if one.Admitted != 0 {
			t.Errorf("a shared-check refusal admitted %d part(s); the refusal stands BEFORE any part exists", one.Admitted)
		}
		if one.Requested < 2 {
			t.Errorf("a shared-check refusal was recorded for %d part(s); two parts are what can repeat one check", one.Requested)
		}
	}
	// AND THE SECOND FIRING, WHERE THERE WAS ONE. The first ask on a node is
	// refused so that a worker able to redraw a boundary does; a worker that asks
	// AGAIN with the same shape is not refused twice — the division is admitted
	// with the family-wide command taken off every part and carried by the parent
	// instead, and the record says which happened. A run in which the model never
	// asked twice leaves this empty, and that is an honest ending rather than a
	// gap: it means the repair road was not reached, not that it is not there.
	repairs := run.divisionsDeciding(divisionRepairedSharedCheck)
	t.Logf("  the road refused %d division(s) for a shared check and repaired %d", len(refusals), len(repairs))
	for _, one := range repairs {
		if one.Admitted == 0 {
			t.Errorf("a shared-check repair admitted no parts; a repair is an ADMISSION with the command lifted, not another refusal")
		}
		t.Logf("    repaired: requested=%d admitted=%d parts=%v", one.Requested, one.Admitted, one.Parts)
	}
	if len(repairs) == 0 {
		t.Logf("  FINDING: this worker never put the same shape a second time, so the repair road was not reached by this run")
	}

	if len(refusals) > 0 {
		t.Logf("  the road refused %d division(s) for a shared check", len(refusals))
		for _, command := range recordedSharedCommands(t, run.place) {
			t.Logf("    the record names the family-wide command %q", command)
		}
		if !strings.Contains(said, "not split: ") {
			t.Errorf("the road refused this division for a shared check and no journal holds a `not split:` sentence; a refusal the worker cannot read is a refusal it cannot act on")
		}
		if !strings.Contains(said, theWideCheck) {
			t.Errorf("no refusal in this family's journals names %q; a refusal that does not name the command sends the worker back through every done-condition looking for it", theWideCheck)
		}
		if quoted := run.refusalLog(); quoted != "" {
			t.Logf("  what the road told the worker:\n%s", quoted)
		}
	}

	// AND NOTHING WAS BORN OF A REFUSED DIVISION. A refusal is free and admits
	// nothing; the parts that exist are the ones some later division admitted.
	//
	// IT IS READ OFF WHAT A DIVISION ADMITTED AND NEVER OFF ITS DECISION WORD,
	// and that had to be learned out here. This was `len(run.admitted()) == 0`,
	// which counts the decision spelled exactly `admitted` — and the moment the
	// road gained a SECOND way to admit parts (`repaired:shared-check`, which
	// admits the division with the family-wide command lifted off every part) a
	// perfectly correct repair read as three parts born of nothing. The law is
	// about whether any division let parts into the world, so it is asked of the
	// count that says so, and a third admitting decision tomorrow needs no edit
	// here.
	born := false
	for _, one := range run.divisions {
		if one.TaskID == run.root && one.Admitted > 0 {
			born = true
		}
	}
	if !born && len(run.parts()) > 0 {
		t.Errorf("%d part(s) exist under a family in which no division admitted anything:\n%s", len(run.parts()), run.divisionLog())
	}

	// ── what the model then did, measured and reported, asserted nowhere ──
	reportTheReAsk(t, run, nodes)

	// AND HOW THE FAMILY ENDED, REPORTED AND NOT ASSERTED, for the reason
	// [reportTheReAsk] states and one more that had to be measured out here.
	//
	// A REFUSAL IS FREE, BUT ASKING AGAIN IS NOT. The road turns a division down
	// and the worker may ask again; a worker that asks again WITH THE SAME SHAPE
	// spends a step each time and can walk into its own no-progress ceiling
	// holding a finished deliverable. That is what deepseek-v4-flash did on the
	// first fixed run of this scenario — four shared-check refusals, then
	// `stopped: 6 steps without progress`, with a.md, b.md and c.md already
	// written — and it is a fact about how a cheap model reads a refusal, not
	// about whether the road was right to make it. The families lane asserts a
	// worker carries on through the #231 refusal; that assertion is not made
	// here, because making it would put this lane's verdict on #569 in the hands
	// of one model's willingness to redraw a boundary.
	root := run.rootRow()
	t.Logf("  the family ended %s (%s) having written %v", root.State, root.Ending, root.Changed)
	if root.State == string(session.TaskFailed) {
		t.Logf("  FINDING: the family did not reach a settled ending after the refusal — it ran out of steps asking again. Its report was: %s",
			shorten(oneLine(root.Report), 600))
	}
	var home []string
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		if strings.TrimSpace(readGroundFile(t, run.ground, name)) != "" {
			home = append(home, name)
		}
	}
	t.Logf("  the person's folder holds %v of the report's three sections", home)
}

// wideCheckAsk is the person's request. Three disjoint files, one per part, and
// one whole-project check the person asks for in every part's done-condition —
// which is the shape the issue was reported against, written the way somebody
// who had not thought about worktrees would write it.
const wideCheckAsk = `Write a three-section report into the folder this conversation is already about.
The folder holds README.md, which lists the nine topics — read it, and say in every
part's brief that the part reads it too. The report lands as three separate files:
a.md, b.md and c.md — a.md holds the first three topics, b.md the next three, c.md
the last three, about eighty words each. The three sections do not depend on each
other, so split this with divide_work into THREE parts, one file each.

Write every part's done-condition as TWO clauses with a semicolon between them.

The FIRST clause names the ONE file that part produces — a.md, b.md or c.md — and no
other file, because two parts whose done-conditions name the same file are two parts
claiming one file. README.md is shared: every part reads it and no part owns it, so
it belongs in the briefs and in no done-condition.

The SECOND clause is the whole report's own check, and it is the SAME in every part,
spelled exactly:

    ` + theWideCheck + `

Put that sentence in all three done-conditions, word for word, so that no part is
finished until the whole report is there.`

// theWideCheckShapeWasReached says whether this attempt got as far as the thing
// #569 is about: a division that put one whole-project command into more than
// one part's done-condition. It is true where the road turned that division down
// for the shared check (the fix) and true where it admitted the children carrying
// it (the defect), and false only where the model never wrote the shape at all —
// which is what earns the retry.
func theWideCheckShapeWasReached(run *familyRun) bool {
	if len(run.divisionsDeciding(divisionRefusedSharedCheck)) > 0 {
		return true
	}
	nodes := readContractNodes(run.t, run.place.Tasks())
	return len(childrenOrdering(nodes, run.root, theWideCheck)) > 1
}

// reportTheReAsk measures the road AFTER the refusal and asserts nothing at all.
//
// THE END STATE THE FIX IS FOR is three admitted children each carrying a check
// over its own slice, none of them carrying the family-wide one, and that
// family-wide run made ONCE by the parent after its parts' work is home. Whether
// a cheap model gets there is not something this lane can make true, and an
// assertion that quietly relaxed until it passed would be worse than no
// assertion — so every part of it is measured and written down in the log, in
// the model's own words, for whoever reads the run.
func reportTheReAsk(t *testing.T, run *familyRun, nodes []contractNode) {
	t.Helper()
	children := childrenOf(nodes, run.root)
	t.Logf("  THE RE-ASK, as it actually went: %d admitted child(ren) under this family", len(children))
	for _, one := range children {
		t.Logf("    part #%d %q is done when: %s", one.ID, one.Title, shorten(oneLine(one.Acceptance), 400))
	}
	distinct := map[string]bool{}
	for _, one := range children {
		distinct[oneLine(one.Acceptance)] = true
	}
	switch {
	case len(children) == 0:
		t.Logf("    → the worker kept the work in its own hands after the refusal")
	case len(distinct) == len(children):
		t.Logf("    → every one of the %d children was finished against a done-condition of its own", len(children))
	default:
		t.Logf("    → %d children share %d distinct done-condition(s); they were not each given a check over their own slice", len(children), len(distinct))
	}
	for _, one := range children {
		if !strings.Contains(oneLine(one.Acceptance), theWideCheck) && strings.Contains(oneLine(one.Acceptance), theWideCommand) {
			t.Logf("    → part #%d was ordered a check of its own opening with %q rather than the family's sentence", one.ID, theWideCommand)
		}
	}
	// AND THE FAMILY-WIDE RUN, WHERE THE PARENT MADE IT. It is looked for in the
	// parent's own journal, as a tool call carrying the command, because that is
	// the only place a run the parent made is recorded.
	parent := journalOf(t, run.place, run.root)
	if calls := callsCarrying(parent, theWideCommand); len(calls) > 0 {
		t.Logf("    → this task ran the family-wide check %d time(s) itself: %s", len(calls), shorten(strings.Join(calls, " | "), 600))
	} else {
		t.Logf("    → this task never ran %q itself", theWideCommand)
	}
	for _, one := range children {
		if calls := callsCarrying(journalOf(t, run.place, one.ID), theWideCommand); len(calls) > 0 {
			t.Logf("    → part #%d ran the family-wide check %d time(s) in its own tree: %s",
				one.ID, len(calls), shorten(strings.Join(calls, " | "), 400))
		}
	}
}

// ── readers this file adds ──────────────────────────────────────────────────

// contractNode is one node of the graph's checkpoint read for the half
// [taskRow] does not carry: the contract it was given. The two are separate
// structs on purpose — [taskRow] is what the family lane asserts a node's SHAPE
// with, and this is what a done-condition is read out of — and both decode the
// same file (task_store.go's taskRecord).
type contractNode struct {
	ID          uint64 `json:"id"`
	Parent      uint64 `json:"parent"`
	Title       string `json:"title"`
	State       string `json:"state"`
	Brief       string `json:"brief"`
	Deliverable string `json:"deliverable"`
	Acceptance  string `json:"acceptance"`
	Where       string `json:"where"`
	Journal     string `json:"journal"`
}

func readContractNodes(t *testing.T, path string) []contractNode {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var document struct {
		Nodes []contractNode `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return document.Nodes
}

// childrenOf is every node handed out under one parent, in id order.
func childrenOf(nodes []contractNode, parent uint64) []contractNode {
	var out []contractNode
	for _, one := range nodes {
		if one.Parent == parent {
			out = append(out, one)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

// childrenOrdering is every child of one parent whose done-condition orders a
// given command.
//
// THE MATCH IS ON THE COMMAND'S OWN WORDS, with the backticks a model may have
// wrapped it in taken off and runs of whitespace flattened — the two differences
// between one spelling of a command and another that change nothing about what
// would run. Everything else is kept, because every other word narrows what runs
// and folding two narrowed checks together would count a check nobody ordered.
func childrenOrdering(nodes []contractNode, parent uint64, command string) []contractNode {
	var out []contractNode
	for _, one := range childrenOf(nodes, parent) {
		if strings.Contains(oneLine(one.Acceptance), oneLine(command)) {
			out = append(out, one)
		}
	}
	return out
}

// oneLine is a done-condition with its backticks off and its whitespace
// flattened, which is the form [childrenOrdering] compares.
func oneLine(text string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(text, "`", "")), " ")
}

// recordedSharedCommands is every command the division road wrote down as
// standing in more than one part's done-condition (sessionfile.go's
// journalDivision.Shared). It is read straight off the journal rather than
// through [readDivisions] because that reader carries the decision and not this
// field, and an autopsy asking WHICH check was family-wide should read it off
// the line rather than count shell calls across four worktrees.
func recordedSharedCommands(t *testing.T, place session.Place) []string {
	t.Helper()
	seen := map[string]bool{}
	var out []string
	for _, path := range nodeJournals(t, place) {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.Contains(line, `"division"`) {
				continue
			}
			var entry struct {
				Division *struct {
					Shared []string `json:"shared"`
				} `json:"division"`
			}
			if err := json.Unmarshal([]byte(line), &entry); err != nil || entry.Division == nil {
				continue
			}
			for _, command := range entry.Division.Shared {
				if !seen[command] {
					seen[command] = true
					out = append(out, command)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// journalOf is one node's own transcript, whole. The file is named
// `<stamp>_<id>.jsonl` (task_run.go's findTaskJournal) and the audits and repair
// rounds that sit beside it under the same id carry more after the id, so the
// suffix match is exact.
func journalOf(t *testing.T, place session.Place, id uint64) string {
	t.Helper()
	want := fmt.Sprintf("_%d.jsonl", id)
	var whole strings.Builder
	for _, path := range nodeJournals(t, place) {
		if !strings.HasSuffix(filepath.Base(path), want) {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		whole.Write(raw)
	}
	return whole.String()
}

// openingMessage is the first thing a node was ever asked — the contract, as the
// worker received it — read out of that node's own journal.
func openingMessage(t *testing.T, place session.Place, id uint64) string {
	t.Helper()
	for _, line := range strings.Split(journalOf(t, place, id), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Type == "message" && entry.Role == "user" {
			return entry.Content
		}
	}
	return ""
}

// callsCarrying is every tool call in one journal whose arguments carry a
// command, as the arguments were actually sent. It is how a run somebody made is
// read back: a shell call is the only record that a command was ordered, and the
// arguments are where the command is.
func callsCarrying(journal, command string) []string {
	var out []string
	flat := oneLine(command)
	for _, line := range strings.Split(journal, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry struct {
			ToolCalls []struct {
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"toolCalls"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		for _, one := range entry.ToolCalls {
			if strings.Contains(oneLine(one.Function.Arguments), flat) {
				out = append(out, one.Function.Name+" "+shorten(oneLine(one.Function.Arguments), 200))
			}
		}
	}
	return out
}

// awaitRootNode waits for the graph to hold a node nobody handed out — the task
// the conversation proposed — and answers its id, or 0 where the chat proposed
// nothing before the wall.
func awaitRootNode(ctx context.Context, t *testing.T, place session.Place) uint64 {
	t.Helper()
	for {
		root := uint64(0)
		for _, row := range readTaskRows(t, place.Tasks()) {
			if row.Parent == 0 && (root == 0 || row.ID < root) {
				root = row.ID
			}
		}
		if root != 0 {
			return root
		}
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(time.Second):
		}
	}
}

// ── watching a file that will not survive the run ───────────────────────────

// copyWatch is one file polled while the work runs, because the two directories
// this lane is about do not both outlive it: a landed task's worktree is
// REMOVED, so what its copy held has to be taken while the worker is standing in
// it. It records the last content seen at each matching path, and the ordered
// sequence of DISTINCT contents — which is how "did their own file change while
// somebody else's work ran" is answered at all.
type copyWatch struct {
	mu      sync.Mutex
	last    map[string]string
	at      string
	changes []string
	halt    chan struct{}
	once    sync.Once
}

// watchFile starts one. The pattern is joined onto root and read as a glob, so
// one watcher can follow `trees/*/internal/widget.md` across however many copies
// the run cuts.
func watchFile(root, pattern string) *copyWatch {
	watch := &copyWatch{last: map[string]string{}, halt: make(chan struct{})}
	go func() {
		for {
			found, _ := filepath.Glob(filepath.Join(root, pattern))
			for _, path := range found {
				raw, err := os.ReadFile(path)
				if err != nil {
					continue
				}
				watch.mu.Lock()
				if watch.last[path] != string(raw) {
					watch.last[path] = string(raw)
					watch.at = path
					if len(watch.changes) == 0 || watch.changes[len(watch.changes)-1] != string(raw) {
						watch.changes = append(watch.changes, string(raw))
					}
				}
				watch.mu.Unlock()
			}
			select {
			case <-watch.halt:
				return
			case <-time.After(250 * time.Millisecond):
			}
		}
	}()
	return watch
}

func (c *copyWatch) stop() { c.once.Do(func() { close(c.halt) }) }

// lastSeen is the newest content seen at any matching path, and that path.
func (c *copyWatch) lastSeen() (string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.at == "" {
		return "", ""
	}
	return c.last[c.at], c.at
}

// steps is the ordered sequence of distinct contents this file was seen holding,
// which is what "how many times did it change" is read from.
func (c *copyWatch) steps() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.changes))
	copy(out, c.changes)
	return out
}

// quoteAll spells a sequence of file contents for a log line.
func quoteAll(steps []string) []string {
	out := make([]string, 0, len(steps))
	for _, step := range steps {
		out = append(out, fmt.Sprintf("%q", shorten(strings.TrimSpace(step), 120)))
	}
	return out
}

// quoteAround is the few lines of a transcript on either side of a sentence,
// which is what an autopsy of a refusal reads.
func quoteAround(text, needle string, around int) string {
	all := strings.Split(text, "\n")
	for index, line := range all {
		if !strings.Contains(line, needle) {
			continue
		}
		from, to := index-around, index+around+1
		if from < 0 {
			from = 0
		}
		if to > len(all) {
			to = len(all)
		}
		var out []string
		for _, one := range all[from:to] {
			out = append(out, "    "+shorten(one, 600))
		}
		return strings.Join(out, "\n")
	}
	return ""
}

// toolTraffic is every tool call one node made and every answer it was handed,
// in order and clipped — the record of the addresses a worker reached for. It is
// logged only where the work did not land, because that is the one failure whose
// evidence lives in a directory the landing has already removed.
func toolTraffic(journal string) string {
	var out []string
	for _, line := range strings.Split(journal, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry struct {
			Type      string `json:"type"`
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"toolCalls"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil || entry.Type != "message" {
			continue
		}
		for _, one := range entry.ToolCalls {
			out = append(out, "    CALL "+one.Function.Name+" "+shorten(oneLine(one.Function.Arguments), 300))
		}
		if entry.Role == "tool" {
			out = append(out, "      → "+shorten(oneLine(entry.Content), 300))
		}
	}
	if len(out) == 0 {
		return "    (this node's journal records no tool traffic at all)"
	}
	return strings.Join(out, "\n")
}
