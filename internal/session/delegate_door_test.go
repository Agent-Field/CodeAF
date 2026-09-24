package session

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// testPrograms is a build that carries one program called name. The session
// never starts its process — the run engine is a double here — so its command
// is a body that is never called.
func testPrograms(name string) []delegate.Delegate {
	return []delegate.Delegate{{
		Name: name, Summary: "a fake program", Default: "run", Page: name,
		Guide: "For work a fake does, with a brief that names the fake's files.",
		Commands: []delegate.Command{{Name: "run", Bind: func(*flag.FlagSet) delegate.Body {
			return func(context.Context, delegate.Host, []string) error { return nil }
		}}},
	}}
}

// The whole road from the door to the branch: `/fake <brief>` starts a run
// whose spec names the delegate, the program's own commits in the copy are
// squashed into ONE commit whose subject is the task's title and whose body is
// the run's result, and that commit lands AS ITS BRANCH in the repository the
// copy was cut from — never merged into the person's checkout. The engine is a
// double whose `work` hook plays the program: two files, two commits, the way
// senior-dev commits every edit.
func TestADelegatedRunSquashesTheProgramsCommitsAndLandsThemAsABranch(t *testing.T) {
	// The double answers the run's result off the completer it is handed, so
	// the result is scripted there: the sentence the landing commit must carry.
	const result = "submitted and verified. fake's model said: tests pass"
	double := newBeltRunDouble(result)
	double.work = func(workspace string) {
		for _, name := range []string{"one.txt", "two.txt"} {
			if err := os.WriteFile(filepath.Join(workspace, name), []byte(name+"\n"), 0o644); err != nil {
				t.Error(err)
				return
			}
			mustGit(t, workspace, "add", name)
			mustGit(t, workspace, "-c", "user.name=p", "-c", "user.email=p@p", "commit", "-q", "-m", "wip(edit): "+name)
		}
	}
	registerBeltRunEngine(t, double)
	conversation := newTestRepo(t)
	base := strings.TrimSpace(gitOut(t, conversation, "rev-parse", "HEAD"))
	sessionDir := t.TempDir()
	registry := testPrograms("fake")
	agent, _ := newTestAgent(t, beltRunCompleter{text: result}, func(config *Config) {
		config.Workspace = conversation
		config.Place = Place{Dir: sessionDir}
		config.AskConsent = false
		config.Delegates = registry
	})

	id, title, note, err := agent.StartDelegate(context.Background(), "fake", "add two files to the project")
	if err != nil {
		t.Fatalf("StartDelegate: %v", err)
	}
	if id == 0 || title == "" || note != "" {
		t.Fatalf("StartDelegate answered id %d title %q note %q", id, title, note)
	}
	<-double.entered
	double.mu.Lock()
	spec := double.spec
	double.mu.Unlock()
	if spec.Delegate == nil || spec.Delegate.Name != "fake" {
		t.Fatalf("the engine was handed no delegate: %+v", spec.Delegate)
	}
	if spec.Brief != "add two files to the project" {
		t.Fatalf("brief = %q", spec.Brief)
	}
	// The folder the task was proposed on is handed over in its spellings, so
	// the program's brief names its copy wherever it named the folder.
	if !slices.Contains(spec.Ground, canonicalPath(conversation)) || canonicalPath(spec.Workspace) == canonicalPath(conversation) {
		t.Fatalf("spec.Ground = %q for a copy at %q, want the proposed folder's spellings", spec.Ground, spec.Workspace)
	}
	endBeltRun(t, agent, double)

	// THE CHECKOUT IS UNTOUCHED: nothing was merged into it.
	if head := strings.TrimSpace(gitOut(t, conversation, "rev-parse", "HEAD")); head != base {
		t.Fatalf("the person's checkout moved from %s to %s; a program's work lands as its branch", base, head)
	}
	for _, name := range []string{"one.txt", "two.txt"} {
		if _, err := os.Stat(filepath.Join(conversation, name)); !os.IsNotExist(err) {
			t.Fatalf("%s was written into the person's checkout: %v", name, err)
		}
	}
	// ONE COMMIT ON THE TASK'S BRANCH ABOVE THE BASE, and it is codeaf's
	// landing commit, not the program's two.
	branches := strings.Fields(gitOut(t, conversation, "branch", "--format=%(refname:short)", "--list", "task/*"))
	if len(branches) != 1 {
		t.Fatalf("want the task's one branch in the repository, got %q", branches)
	}
	log := gitOut(t, conversation, "log", "--format=%s%n%b", base+".."+branches[0])
	subjects := strings.Fields(gitOut(t, conversation, "rev-list", base+".."+branches[0]))
	if len(subjects) != 1 || strings.Contains(log, "wip(edit)") || !strings.HasPrefix(log, "task: ") {
		t.Fatalf("the branch holds %d commits above the base, want one `task:` commit:\n%s", len(subjects), log)
	}
	if !strings.Contains(log, "fake's model said: tests pass") {
		t.Fatalf("the landing commit's body does not carry the run's result:\n%s", log)
	}
	// AND THE PAGE SAYS WHERE IT IS.
	store := beltRunStoreAt(t, filepath.Dir(spec.Store.Path()))
	defer store.Close()
	var said []string
	for _, n := range store.Notes(store.RootID(), 0) {
		said = append(said, n.Body)
	}
	if joined := strings.Join(said, "\n"); !strings.Contains(joined, "its work is on the branch "+branches[0]) ||
		!strings.Contains(joined, "nothing was merged into your checkout") {
		t.Fatalf("the run's notes = %q, want the branch it landed on", said)
	}
}

// A FOLDER WITH NO GIT HISTORY: the program is told so on its line, works in
// the folder itself because there is nothing to copy from, and its landing
// commits nothing — no repository is made in the person's folder — and says
// where the work is instead of refusing a commit git could never make.
func TestADelegatedRunOnAPlainFolderIsToldSoAndLandsWhereItWorked(t *testing.T) {
	const result = "submitted and verified. fake's model said: done"
	double := newBeltRunDouble(result)
	double.work = func(workspace string) {
		if err := os.WriteFile(filepath.Join(workspace, "made.txt"), []byte("made\n"), 0o644); err != nil {
			t.Error(err)
		}
	}
	registerBeltRunEngine(t, double)
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "notes.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, beltRunCompleter{text: result}, func(config *Config) {
		config.Workspace = folder
		config.Place = Place{Dir: t.TempDir()}
		config.AskConsent = false
		config.Delegates = testPrograms("fake")
	})

	if _, _, _, err := agent.StartDelegate(context.Background(), "fake", "make a file in this folder"); err != nil {
		t.Fatalf("StartDelegate: %v", err)
	}
	<-double.entered
	double.mu.Lock()
	spec := double.spec
	double.mu.Unlock()
	if !spec.PlainFolder || canonicalPath(spec.Workspace) != canonicalPath(folder) || len(spec.Ground) != 0 {
		t.Fatalf("spec = plain %v in %q, ground %q, want the plain folder itself, said to be one, and nothing to rewrite", spec.PlainFolder, spec.Workspace, spec.Ground)
	}
	endBeltRun(t, agent, double)

	if content, err := os.ReadFile(filepath.Join(folder, "made.txt")); err != nil || string(content) != "made\n" {
		t.Fatalf("the work is not in the folder: %q %v", content, err)
	}
	if _, err := os.Stat(filepath.Join(folder, ".git")); !os.IsNotExist(err) {
		t.Fatalf("the landing made the plain folder a repository: %v", err)
	}
	store := beltRunStoreAt(t, filepath.Dir(spec.Store.Path()))
	defer store.Close()
	var said []string
	for _, note := range store.Notes(store.RootID(), 0) {
		said = append(said, note.Body)
	}
	joined := strings.Join(said, "\n")
	if !strings.Contains(joined, "no git history, so nothing was committed") || strings.Contains(joined, "not a git repository") {
		t.Fatalf("the run's notes = %q, want the plain-folder landing and no git refusal", said)
	}
}

// A folder with history is copied, and the program is told nothing extra.
func TestADelegatedRunOnARepositoryIsNotToldItIsPlain(t *testing.T) {
	double := newBeltRunDouble("done")
	registerBeltRunEngine(t, double)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: t.TempDir()}
		config.AskConsent = false
		config.Delegates = testPrograms("fake")
	})
	if _, _, _, err := agent.StartDelegate(context.Background(), "fake", "change the project"); err != nil {
		t.Fatalf("StartDelegate: %v", err)
	}
	<-double.entered
	double.mu.Lock()
	plain := double.spec.PlainFolder
	double.mu.Unlock()
	endBeltRun(t, agent, double)
	if plain {
		t.Fatal("a repository with a commit was called a plain folder")
	}
}

// A program that ended without finishing is drawn from its own words: the row
// names what it said, with no fault in front of it, and a crash is the fault
// it is. The row that read "a fault: ran and did not finish" over an hour of
// work that had said exactly why it would not stand told a person nothing.
func TestAProgramsOwnEndingIsTheRowsReasonAndNotAFault(t *testing.T) {
	agent, _ := newTestAgent(t, beltRunCompleter{text: "unused"}, func(config *Config) {})
	program := testPrograms("fake")[0]
	run := &beltRun{row: 3, title: "the task", delegate: &program}
	summary := RunSummary{Outcome: "ran and did not finish", Program: &ProgramEnding{
		Status: delegate.StatusFail,
		Reason: "fake did not finish: submitted a change the project's own tests do not pass",
		Result: "submitted a change the project's own tests do not pass. fake's model said: done",
	}}
	notice := agent.beltRunNotice(run, summary, RunLanding{})
	if notice.State != TaskFailed || notice.Ending != TaskEndingProgram {
		t.Fatalf("notice = %s / %q, want failed on the program's own ending", notice.State, notice.Ending)
	}
	if reason := TaskReasonOf(notice.Ending, notice.Report); reason != summary.Program.Reason {
		t.Fatalf("reason = %q, want the program's sentence %q", reason, summary.Program.Reason)
	}
	if taskEndingIsFault(notice.Ending) {
		t.Fatal("a program judging its own work unfinished was drawn as a fault")
	}
	if note := beltRunOutcomeNote(nil, "", summary, RunLanding{}, 0); !strings.HasPrefix(note, summary.Program.Reason) || strings.Contains(note, "ran and did not finish") {
		t.Fatalf("the outcome note = %q, want the program's own words and not the run's generic one", note)
	}
	summary.Program.Status = delegate.StatusCrashed
	if notice := agent.beltRunNotice(run, summary, RunLanding{}); notice.Ending != TaskEndingError {
		t.Fatalf("a crash ended %q, want the fault it is", notice.Ending)
	}
}

// A program is handed the conversation's crew — its planning, working and
// light seats, with any effort taken off — and a run no program works is handed
// none.
func TestAProgramIsHandedTheConversationsCrew(t *testing.T) {
	agent, _ := newTestAgent(t, beltRunCompleter{text: "unused"}, func(config *Config) {
		config.RolesSource = tierSettings(map[string]string{
			"tiers.mastermind": "vendor/brain:high",
			"tiers.worker":     "vendor/hands",
			"tiers.low":        "vendor/light",
		})
	})
	program := testPrograms("fake")[0]
	got := agent.delegateCrew(&beltRun{delegate: &program})
	if want := (delegate.Crew{Brain: "vendor/brain", Hands: "vendor/hands", Light: "vendor/light"}); got != want {
		t.Fatalf("crew = %+v, want %+v", got, want)
	}
	if got := agent.delegateCrew(&beltRun{}); !got.IsZero() {
		t.Fatalf("a run no program works was handed a crew: %+v", got)
	}
}

// A PROGRAM'S RUN THAT DID NOT FINISH IS OVER ON ITS PAGE. The engine left its
// store open, the page read `running · … · x stop it` for forty minutes over a
// program that had ended, and the next hand-off would have adopted it. Its
// store's run task is now ended with the program's own sentence.
func TestAProgramsRunThatDidNotFinishIsEndedInItsStore(t *testing.T) {
	double := newBeltRunDouble("")
	double.leaveOpen = true
	double.summary = RunSummary{Outcome: "ran and did not finish", Program: &ProgramEnding{
		Status: delegate.StatusFail, Reason: "fake did not finish: its tests fail", Result: "its tests fail",
	}}
	registerBeltRunEngine(t, double)
	agent, _ := newTestAgent(t, beltRunCompleter{text: ""}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: t.TempDir()}
		config.AskConsent = false
		config.Delegates = testPrograms("fake")
	})
	id, _, _, err := agent.StartDelegate(context.Background(), "fake", "change the project")
	if err != nil {
		t.Fatalf("StartDelegate: %v", err)
	}
	<-double.entered
	double.mu.Lock()
	spec := double.spec
	double.mu.Unlock()
	endBeltRun(t, agent, double)

	store := beltRunStoreAt(t, filepath.Dir(spec.Store.Path()))
	defer store.Close()
	root := store.Task(store.RootID())
	if root.Status != plandb.StatusFailed || root.Error != "fake did not finish: its tests fail" {
		t.Fatalf("the run's task = %s (%q), want failed with the program's own sentence", root.Status, root.Error)
	}
	page, ok := agent.PlanTaskPage(strconv.FormatUint(id, 10))
	if !ok || page.Row.Status != string(plandb.StatusFailed) {
		t.Fatalf("the task's page row = %+v (%v), want it ended and not running", page.Row, ok)
	}
}

func TestStartDelegateRefusesANameThisMachineDoesNotHave(t *testing.T) {
	double := newBeltRunDouble("done")
	registerBeltRunEngine(t, double)
	registry := testPrograms("fake")
	agent, _ := newTestAgent(t, beltRunCompleter{text: "unused"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: t.TempDir()}
		config.Delegates = registry
	})
	_, _, _, err := agent.StartDelegate(context.Background(), "other", "do a thing")
	if err == nil || err.Error() != "this codeaf carries no program called other; it carries fake" {
		t.Fatalf("err = %v", err)
	}
	if double.didRun() {
		t.Fatal("a refused delegate started a run")
	}
	// And a build that carries none says so plainly.
	none, _ := newTestAgent(t, beltRunCompleter{text: "unused"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: t.TempDir()}
	})
	_, _, _, err = none.StartDelegate(context.Background(), "fake", "do a thing")
	if err == nil || err.Error() != "this codeaf carries no program called fake" {
		t.Fatalf("err = %v", err)
	}
}

// A DELEGATE RUNS ALONE. A second hand-off while a delegated run is going is
// refused with the folder that is busy, and a delegate proposed while an
// ordinary run is going is refused the same way.
func TestNothingJoinsADelegatedRunAndADelegateJoinsNothing(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("done")
	double.honoursStop = true
	registerBeltRunEngine(t, double)
	conversation := newTestRepo(t)
	registry := testPrograms("fake")
	agent, _ := newTestAgent(t, beltRunCompleter{text: "unused"}, func(config *Config) {
		config.Workspace = conversation
		config.Place = Place{Dir: t.TempDir()}
		config.AskConsent = false
		config.Delegates = registry
	})
	if _, _, _, err := agent.StartDelegate(context.Background(), "fake", "the delegated work"); err != nil {
		t.Fatal(err)
	}
	<-double.entered
	stand := taskStand{dir: conversation, mode: TaskModeWorktree}
	err := agent.startKnownTaskRun(context.Background(), 99, "a second piece", "brief", nil, stand, "")
	if err == nil || !strings.Contains(err.Error(), "fake runs alone") {
		t.Fatalf("a task joined a delegated run: %v", err)
	}
	endBeltRun(t, agent, double)
}

// The prompt names the programs this build carries, and only where there are
// some: a conversation with one reads its name and its own guide under the
// hand-off facts, and one without reads nothing about them at all.
func TestThePromptNamesTheDelegatesThisLaunchHasAndOnlyThose(t *testing.T) {
	with := Config{Workspace: t.TempDir(), Delegates: testPrograms("fake")}
	page := promptWithBeltFacts(with)
	if !strings.Contains(page, "The programs here:\n- `fake`: "+with.Delegates[0].Guide) {
		t.Fatalf("the page does not list the delegate with its own guide:\n%s", page)
	}
	if !strings.Contains(page, "`via`") {
		t.Fatal("the page does not say how a delegate is named on a proposal")
	}
	without := Config{Workspace: t.TempDir()}
	if page := promptWithBeltFacts(without); strings.Contains(page, "The programs here") || strings.Contains(page, "PROGRAM BUILT INTO CODEAF") {
		t.Fatalf("a launch with no delegates still speaks of them:\n%s", page)
	}
	inTask := Config{Workspace: t.TempDir(), Delegates: with.Delegates, InTask: true}
	if page := promptWithBeltFacts(inTask); strings.Contains(page, "The programs here") {
		t.Fatal("a task node is told it may delegate")
	}
}

// THE FOLDER A PROGRAM IS HANDED IS CODEAF'S TO EXPLAIN, and it is explained
// only where it is true. A program that edits files works in a copy of the
// proposal's folder and lands only from there, so the page tells the model to
// hand it the repository the work belongs in — cloned first when this machine
// lacks it — and never to brief it to work somewhere else: the failure this
// sentence was written from is senior-dev cloning a repository into the
// person's projects folder because its brief said to. A program that only
// answers works in place and lands nothing, so a build carrying only those is
// told nothing about copies.
func TestTheFolderRuleIsSaidWhereAProgramEditsFilesAndOnlyThere(t *testing.T) {
	tree := Config{Workspace: t.TempDir(), Delegates: testPrograms("fake")}
	page := promptWithBeltFacts(tree)
	for _, want := range []string{
		"It works in a copy of the task's folder, and only that copy's work is kept, on a branch\nnothing merges",
		"clone one this machine\nlacks into a new folder",
		"branch at the commit the work names, and pass it as\n`ground`.",
		"Never brief it to work elsewhere.",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("a build carrying a program that edits files is not told %q:\n%s", want, page)
		}
	}
	textOnly := testPrograms("reader")
	textOnly[0].Lands = delegate.LandsText
	page = promptWithBeltFacts(Config{Workspace: t.TempDir(), Delegates: textOnly})
	if !strings.Contains(page, "- `reader`: ") {
		t.Fatalf("the program that answers is not listed:\n%s", page)
	}
	if strings.Contains(page, "copy of the task's folder") {
		t.Fatalf("a build whose only program works in place is told about copies:\n%s", page)
	}
}

// THE PAGE SAYS NOTHING ABOUT A PROGRAM THAT THE PROGRAM DOES NOT SAY. Two
// programs are listed in name order, each with its own guide and nobody
// else's, so a second program joins the page by bringing its guide and never
// by an edit to the conversation's words.
func TestEachProgramIsListedWithItsOwnGuideInNameOrder(t *testing.T) {
	programs := append(testPrograms("zeta"), testPrograms("alpha")...)
	programs[0].Guide = "For the zeta work."
	programs[1].Guide = "For the alpha work."
	page := promptWithBeltFacts(Config{Workspace: t.TempDir(), Delegates: programs})
	want := "The programs here:\n- `alpha`: For the alpha work.\n- `zeta`: For the zeta work."
	if !strings.Contains(page, want) {
		t.Fatalf("the page does not list both programs with their own guides in order; want %q in:\n%s", want, page)
	}
}

// A PROGRAM'S PAGE IS READ WITH THE SWITCH OFF. `/senior-dev` takes the run
// road whatever CODEAF_TASK_BELT says, and its store is written either way, so
// the pages that read that store must answer either way: with the readers
// gated on the switch, a person on the default belt clicked into senior-dev's
// task and got a room that said it would fill in, for the whole run.
func TestAProgramsRunIsReadableWithTheSwitchOff(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "")
	if bashBeltAsked() {
		t.Fatal("the switch is still on, so this test would prove nothing")
	}
	double := newBeltRunDouble("done")
	registerBeltRunEngine(t, double)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: t.TempDir()}
		config.AskConsent = false
		config.Delegates = testPrograms("fake")
	})
	if _, _, _, err := agent.StartDelegate(context.Background(), "fake", "add a file"); err != nil {
		t.Fatal(err)
	}
	<-double.entered
	var program PlanTaskRow
	for _, row := range agent.PlanTasks() {
		if row.Program == "fake" {
			program = row
		}
	}
	if program.ID == "" {
		t.Fatalf("the program's run has no row with the switch off: %+v", agent.PlanTasks())
	}
	page, found := agent.PlanTaskPage(program.ID)
	if !found || page.Program == nil || page.Program.Name != "fake" {
		t.Fatalf("the program's page is not readable with the switch off: found %v, program %+v", found, page.Program)
	}
	// AND NOTHING WAS ARMED: the switch's own roads stay closed to every
	// ordinary task of this conversation.
	if g := agent.graph(); g != nil && g.planIfArmed() != nil {
		t.Fatal("reading the program's page armed the plan for the switch's other roads")
	}
	endBeltRun(t, agent, double)
}
