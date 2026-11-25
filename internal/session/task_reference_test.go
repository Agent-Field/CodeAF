package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// A TASK THAT ONLY READS ITS GROUND SAYS SO AT ALL THREE ENDS: the proposer's
// receipt, the worker's own words, and the landing. The run this is from: a
// proposal gave a repository as its ground and named no file by path, the
// worker built the project in its own empty folder and reported it
// "integrated", and nothing told anybody the repository was untouched.
func TestAReferenceTaskSaysItsGroundDoesNotChange(t *testing.T) {
	root := t.TempDir()
	ground, own := filepath.Join(root, "MacStats"), filepath.Join(root, "trees", "1")

	said := taskStandSentence(taskStand{dir: ground, rung: taskGroundSaid, mode: TaskModeReference})
	for _, want := range []string{"It reads " + ground, "nothing in " + ground + " changes", "propose it again naming the files it makes or changes by their full path under " + ground} {
		if !strings.Contains(said, want) {
			t.Errorf("the receipt lacks %q:\n%s", want, said)
		}
	}
	if strings.Contains(said, "It works in") {
		t.Errorf("the receipt still says the task works in the ground it only reads:\n%s", said)
	}
	if got := taskStandSentence(taskStand{dir: ground, rung: taskGroundSaid, mode: TaskModeWorktree}); !strings.HasPrefix(got, "It works in "+ground) {
		t.Errorf("a task that works in its ground is said as %q", got)
	}

	note := copyOnto(ground, TaskModeReference, own, Place{}).ownFolderNote()
	for _, want := range []string{ground + " is yours to read, not to change", "Everything you make goes in " + own, "never say the work is in " + ground} {
		if !strings.Contains(note, want) {
			t.Errorf("the worker's note lacks %q:\n%s", want, note)
		}
	}
	if got := copyOnto(ground, TaskModeReference, ground, Place{}).ownFolderNote(); got != "" {
		t.Errorf("a reference standing in its own ground was given a note: %q", got)
	}
	if got := copyOnto(ground, TaskModeWorktree, own, Place{}).ownFolderNote(); got != "" {
		t.Errorf("a worktree was told its ground is read-only: %q", got)
	}

	line := referenceLandingLine(taskTree{dir: own, ground: ground, mode: TaskModeReference})
	if line != "it changed nothing in "+ground+": what it made is in "+own {
		t.Errorf("the landing line is %q", line)
	}
	for _, tree := range []taskTree{
		{dir: own, ground: ground, mode: TaskModeWorktree},
		{dir: ground, ground: ground, mode: TaskModeReference},
		{dir: own, mode: TaskModeReference},
	} {
		if got := referenceLandingLine(tree); got != "" {
			t.Errorf("%+v was given a reference landing: %q", tree, got)
		}
	}

	brief := withTaskReadsNote(Place{}, "Build it.", taskStand{dir: ground, mode: TaskModeReference})
	if !strings.Contains(brief, ground+" is yours to read, not to change") || !strings.Contains(brief, "your assigned working directory") {
		t.Errorf("a run's workers are not told their ground is read-only:\n%s", brief)
	}
	if got := withTaskReadsNote(Place{}, "Build it.", taskStand{dir: ground, mode: TaskModeWorktree}); got != "Build it." {
		t.Errorf("a run that works in its ground was told otherwise:\n%s", got)
	}
}

// A MANAGER IS TOLD A TASK IS NOT A WAY ROUND A MEMBER THAT DID NOT START.
func TestTheManagersLawsSayWhenATaskIsUsed(t *testing.T) {
	for _, want := range []string{"A task (propose_task) is not a member", "never to get round a member that did not start"} {
		if !strings.Contains(teamManagerLaws, want) {
			t.Errorf("the manager's laws lack %q", want)
		}
	}
}

// THE RECEIPT OF A REFERENCE SAYS ONE TRUE THING ABOUT WHERE ITS WORK GOES. A
// declined `in place` leaves a redirect naming a branch on the stand, and a
// reference cuts none; the home folder is said in its own sentence whether a
// `where` door or the proposal's `ground` named it.
func TestAReferenceReceiptSaysNoBranchAndTheHomeFolderOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ground := filepath.Join(t.TempDir(), "MacStats")
	spec := taskSpec{title: "the question"}
	branch := whereRedirectSentence("in place", ground)

	reads := taskReceipt(7, spec, TaskRunning, taskStand{dir: ground, mode: TaskModeReference, rung: taskGroundSaid, redirect: branch}, "")
	if !strings.Contains(reads, "It reads "+ground) || strings.Contains(reads, "branch cut from it") {
		t.Errorf("a reference asked for in place is told about a branch:\n%s", reads)
	}
	if works := taskReceipt(7, spec, TaskRunning, taskStand{dir: ground, mode: TaskModeWorktree, rung: taskGroundSaid, redirect: branch}, ""); !strings.Contains(works, branch) {
		t.Errorf("a task on a branch lost its redirect:\n%s", works)
	}

	for _, stand := range []taskStand{
		{dir: home, mode: TaskModeReference, rung: taskGroundSaid},
		{dir: home, mode: TaskModeReference, rung: taskGroundNamed, redirect: homeTaskRedirect(home)},
	} {
		for _, state := range []TaskState{TaskRunning, TaskQueued} {
			receipt := taskReceipt(7, spec, state, stand, "")
			if strings.Count(receipt, homeTaskRedirect(home)) != 1 || strings.Contains(receipt, "It works in "+home) {
				t.Errorf("rung %q: the home folder's receipt is\n%s", stand.rung, receipt)
			}
		}
	}
}

// A REFERENCE RUN LANDS WITHOUT THE ENGINE. Its folder is no repository, so the
// engine's landing failed and filed a failure note for every such run; the
// landing is the one line, in the row's report and on the run's page.
//
// AND A TASK THAT JOINS IT IS TOLD THE RUN ONLY READS, whatever its own stand
// asked for: it works in the run's folder, which nothing lands from.
func TestAReferenceRunLandsWithItsLineAndNoEngineLanding(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("the parser reads tokens")
	// The engine's real landing, so that calling it would fail the way it did.
	double.real = true
	registerBeltRunEngine(t, double)
	ground := newTestRepo(t)
	sessionDir := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "the parser reads tokens"}, func(config *Config) {
		config.Workspace = ground
		config.Place = Place{Dir: sessionDir}
		config.SessionFile = filepath.Join(sessionDir, placeTranscript)
		config.AskConsent = false
	})
	if err := agent.startKnownTaskRun(context.Background(), 41, "explain the parser", "brief", nil, taskStand{dir: ground, mode: TaskModeReference, rung: taskGroundSaid}, ""); err != nil {
		t.Fatalf("start the reference run: %v", err)
	}
	<-double.entered
	double.mu.Lock()
	workspace := double.spec.Workspace
	double.mu.Unlock()
	if err := agent.startKnownTaskRun(context.Background(), 42, "change the parser", "brief", nil, taskStand{dir: ground, mode: TaskModeWorktree, rung: taskGroundSaid}, ""); err != nil {
		t.Fatalf("join the reference run: %v", err)
	}
	if task := beltRunTaskAt(t, sessionDir, "42"); task == nil || task.ParentID != "41" || !strings.Contains(task.Description, "is yours to read, not to change") {
		t.Fatalf("a task that joined a run that only reads was not told so: %+v", task)
	}
	endBeltRun(t, agent, double)

	line := referenceLandingLine(taskTree{dir: workspace, ground: canonicalPath(ground), mode: TaskModeReference})
	if line == "" || !strings.Contains(line, "it changed nothing in "+canonicalPath(ground)) {
		t.Fatalf("the run's folder %q and ground %q make no reference landing: %q", workspace, ground, line)
	}
	if double.lands() != 0 {
		t.Errorf("the engine was asked to land a folder that is no repository %d times", double.lands())
	}
	rows := agent.graph().runRows(41)
	if len(rows) != 1 || !strings.Contains(rows[0].Report, line) {
		t.Errorf("the settled row does not carry the landing line %q: %+v", line, rows)
	}
	if notes := strings.Join(beltRunNotes(t, sessionDir, "41"), "\n"); strings.Count(notes, line) != 1 || strings.Contains(notes, "its work is in") {
		t.Errorf("the run's page does not say the landing line alone, once:\n%s", notes)
	}
	if logDir := agent.logDirectory(); logDir == "" {
		t.Error("the conversation has no log directory to read plan notes from")
	} else if log, _ := os.ReadFile(filepath.Join(logDir, "plandb.log")); strings.Contains(string(log), "landing failed") {
		t.Errorf("a landing failure was filed for a run that had nothing to land:\n%s", log)
	}
}

// A REFERENCE THAT JOINS A RUN WORKING IN ITS GROUND IS TOLD WHAT IS TRUE OF
// THAT RUN. It shares the run's copy, which lands into the ground, so neither
// its receipt nor its worker may be told that nothing it makes reaches there.
func TestAReferenceThatJoinsAWritingRunIsNotToldItOnlyReads(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("the run did the work")
	registerBeltRunEngine(t, double)
	sessionDir := t.TempDir()
	ground := newTestRepo(t)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "the run did the work"}, func(config *Config) {
		config.Workspace = ground
		config.Place = Place{Dir: sessionDir}
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	root, _, _, err := agent.StartTask(context.Background(), "open the run", false)
	if err != nil {
		t.Fatal(err)
	}
	<-double.entered
	args, _ := json.Marshal(taskArguments{
		Title: "Explain the parser", Summary: "the proposed work", Brief: "say how the parser reads its input",
		Deliverable: "a concise answer", Acceptance: "the question is answered", Ground: ground,
	})
	proposal, ok := agent.stageTask(context.Background(), args).(*stagedProposal)
	if !ok || proposal.stand.mode != TaskModeReference {
		t.Fatalf("the proposal did not stage as a reference: %+v", proposal)
	}
	agent.ResolveTask(proposal.id, TaskAnswer{Approved: true})
	answer, failed, err := proposal.Commit(context.Background())
	if err != nil || failed {
		t.Fatalf("propose_task: failed=%v err=%v answer=%q", failed, err, answer)
	}
	if !strings.Contains(answer, "It joined the work already underway and shares its copy.") {
		t.Fatalf("the proposal did not join the live run:\n%s", answer)
	}
	for _, untrue := range []string{"It reads ", "nothing in " + canonicalPath(ground) + " changes"} {
		if strings.Contains(answer, untrue) {
			t.Errorf("the receipt of a task that joined a writing run says %q:\n%s", untrue, answer)
		}
	}
	task := beltRunTaskAt(t, sessionDir, strconv.FormatUint(proposal.id, 10))
	if task == nil || task.ParentID != strconv.FormatUint(root, 10) {
		t.Fatalf("the proposal is not a child of the live run: %+v", task)
	}
	if strings.Contains(task.Description, "yours to read") {
		t.Errorf("the worker of a task that joined a writing run is told it only reads:\n%s", task.Description)
	}
	endBeltRun(t, agent, double)
}

// A TASK THAT JOINS A RUN THAT ONLY READS IS TOLD SO AS THE RUN'S DOING. Its
// own proposal named a file by full path, so the starting task's reason ("named
// no file") is false of it, and its remedy ("propose it again") joins the same
// run: the receipt says what is true and the one thing that changes it.
func TestAWritingTaskThatJoinsAReferenceRunIsToldTheRunOnlyReads(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("the parser reads tokens")
	registerBeltRunEngine(t, double)
	sessionDir := t.TempDir()
	ground := newTestRepo(t)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "the parser reads tokens"}, func(config *Config) {
		config.Workspace = ground
		config.Place = Place{Dir: sessionDir}
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	if err := agent.startKnownTaskRun(context.Background(), 41, "explain the parser", "brief", nil, taskStand{dir: ground, mode: TaskModeReference, rung: taskGroundSaid}, ""); err != nil {
		t.Fatalf("start the reference run: %v", err)
	}
	<-double.entered
	file := filepath.Join(canonicalPath(ground), "parser_notes.go")
	args, _ := json.Marshal(taskArguments{
		Title: "Write the parser notes", Summary: "the proposed work", Brief: "write " + file,
		Deliverable: "the file " + file, Acceptance: file + " is there", Ground: ground,
	})
	proposal, ok := agent.stageTask(context.Background(), args).(*stagedProposal)
	if !ok || proposal.stand.mode != TaskModeWorktree {
		t.Fatalf("the proposal did not stage as work in its ground: %+v", proposal)
	}
	agent.ResolveTask(proposal.id, TaskAnswer{Approved: true})
	answer, failed, err := proposal.Commit(context.Background())
	if err != nil || failed {
		t.Fatalf("propose_task: failed=%v err=%v answer=%q", failed, err, answer)
	}
	dir := canonicalPath(ground)
	for _, want := range []string{
		"It joined the work already underway, which only reads " + dir,
		"nothing in " + dir + " changes while it is part of that run",
		"To change " + dir + ", propose the work once that run has ended.",
	} {
		if !strings.Contains(answer, want) {
			t.Errorf("the receipt of a task that joined a run that only reads lacks %q:\n%s", want, answer)
		}
	}
	for _, untrue := range []string{"named no file", "propose it again", "It works in", "shares its copy"} {
		if strings.Contains(answer, untrue) {
			t.Errorf("the receipt of a task that joined a run that only reads says %q:\n%s", untrue, answer)
		}
	}
	endBeltRun(t, agent, double)
}

// A RERUN OF A REFERENCE RUN IS STILL A REFERENCE. The read-only note names the
// ground by full path, and a rerun reads the crew's kept brief again as a
// contract: kept with the note, the rerun stood as a writing task on the
// repository, cut a branch and merged, under words calling it read-only.
func TestARerunOfAReferenceRunIsStillAReference(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	first := newBeltRunDouble("the parser reads tokens")
	registerBeltRunEngine(t, first)
	ground := canonicalPath(newTestRepo(t))
	sessionDir := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "the parser reads tokens"}, func(c *Config) {
		// The conversation stands in a plain folder and is about the repository.
		c.Workspace = t.TempDir()
		c.Place = Place{Dir: sessionDir}
		c.SessionFile = filepath.Join(sessionDir, placeTranscript)
		c.AskConsent = false
		c.ProfileDir = t.TempDir()
		c.RouteCrew = func(config.CrewAsk) (crewroute.Decision, error) { return flashDecision(), nil }
	})
	agent.refer(PlaceRef{Path: ground, Arrival: PlaceSaid, Referred: time.Now()})
	liveStand := func() taskStand {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		if agent.beltRun == nil {
			return taskStand{}
		}
		return agent.beltRun.stand
	}

	id, _, _, err := agent.StartTask(context.Background(), "say how the parser reads its input", false)
	if err != nil {
		t.Fatalf("start the reference run: %v", err)
	}
	<-first.entered
	if stand := liveStand(); stand.dir != ground || stand.mode != TaskModeReference {
		t.Fatalf("the run does not only read %s: %+v", ground, stand)
	}
	first.mu.Lock()
	given := first.spec.Brief
	first.mu.Unlock()
	if !strings.Contains(given, ground+" is yours to read, not to change") {
		t.Errorf("the run's workers are not told their ground is read-only:\n%s", given)
	}
	crew := agent.crews.get(id)
	if crew == nil || strings.Contains(crew.brief, "yours to read") || strings.Contains(crew.brief, ground) {
		t.Fatalf("the brief a rerun reads again carries the read-only note: %+v", crew)
	}
	endBeltRun(t, agent, first)

	second := newBeltRunDouble("the parser reads tokens")
	registerBeltRunEngine(t, second)
	if _, err := agent.rerunWithRouting(context.Background(), TaskNotice{ID: id, Title: crew.title, State: TaskDone}, crew, TaskRoutingAction{Kind: TaskRoutingBest}); err != nil {
		t.Fatalf("rerun the reference run: %v", err)
	}
	<-second.entered
	if stand := liveStand(); stand.dir != ground || stand.mode != TaskModeReference {
		t.Errorf("the rerun of a run that only read %s stands as %+v", ground, stand)
	}
	endBeltRun(t, agent, second)
}
