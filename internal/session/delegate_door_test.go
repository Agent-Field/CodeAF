package session

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/delegate/builtin"
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
// whose spec names the delegate and whose workspace is THE PERSON'S FOLDER
// ITSELF, checked out on a branch codeaf cut for it. The program's own commits
// stay on that branch, what it left uncommitted is committed there in one
// commit whose subject is the task's title and whose body is the run's result,
// the branch is left checked out, and the person's own branch never moves. The
// engine is a double whose `work` hook plays the program: one file committed
// the way senior-dev commits every edit, and one left uncommitted.
func TestADelegatedRunWorksOnItsOwnBranchInTheFolderAndLeavesItCheckedOut(t *testing.T) {
	// The double answers the run's result off the completer it is handed, so
	// the result is scripted there: the sentence the last commit must carry.
	const result = "submitted and verified. fake's model said: tests pass"
	double := newBeltRunDouble(result)
	double.work = func(workspace string) {
		commitIn(t, workspace, "one.txt")
		writeFile(t, filepath.Join(workspace, "two.txt"), "two\n")
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
	// THE PROGRAM WORKS IN THE FOLDER ITSELF, on a branch of its own.
	if canonicalPath(spec.Workspace) != canonicalPath(conversation) || spec.PlainFolder {
		t.Fatalf("the program works in %q (plain %v), want the person's repository %q itself", spec.Workspace, spec.PlainFolder, conversation)
	}
	branch := currentBranch(conversation)
	if !strings.HasPrefix(branch, "task/add-two-files-to-the-project-") {
		t.Fatalf("the checkout is on %q while the program works, want a task branch of its own", branch)
	}
	endBeltRun(t, agent, double)

	// THE PERSON'S BRANCH NEVER MOVED, and the program's branch is left checked
	// out with the work in the folder.
	if tip := strings.TrimSpace(gitOut(t, conversation, "rev-parse", "work")); tip != base {
		t.Fatalf("the person's branch moved from %s to %s", base, tip)
	}
	if head := currentBranch(conversation); head != branch {
		t.Fatalf("the checkout is on %q after the run, want the program's branch %q left checked out", head, branch)
	}
	for _, name := range []string{"one.txt", "two.txt"} {
		if _, err := os.Stat(filepath.Join(conversation, name)); err != nil {
			t.Fatalf("%s is not in the person's folder: %v", name, err)
		}
	}
	if status := strings.TrimSpace(gitOut(t, conversation, "status", "--porcelain")); status != "" {
		t.Fatalf("the run left the folder with changes that are not committed:\n%s", status)
	}
	// THE PROGRAM'S COMMIT STAYS, and codeaf's one commit of what was left is on
	// top of it: the title, then the result.
	subjects := strings.Fields(strings.ReplaceAll(gitOut(t, conversation, "log", "--format=%s", base+".."+branch), " ", "_"))
	if len(subjects) != 2 || subjects[1] != "wip(edit):_one.txt" || !strings.HasPrefix(subjects[0], "add_two_files") {
		t.Fatalf("the branch holds %q, want the program's commit under one commit of the title", subjects)
	}
	if body := gitOut(t, conversation, "log", "-1", "--format=%b", branch); !strings.Contains(body, "fake's model said: tests pass") {
		t.Fatalf("the last commit's body does not carry the run's result:\n%s", body)
	}
	// AND THE PAGE AND THE CONVERSATION SAY WHERE IT IS AND HOW TO GO BACK.
	store := beltRunStoreAt(t, filepath.Dir(spec.Store.Path()))
	defer store.Close()
	var said []string
	for _, n := range store.Notes(store.RootID(), 0) {
		said = append(said, n.Body)
	}
	root := canonicalPath(conversation)
	want := "its work is on the branch " + branch + " in " + root + ", 2 files, and that branch is checked out there; your branch work is as it was: `git -C '" +
		root + "' switch work` goes back to it, and `git -C '" + root + "' merge " + branch + "` from there brings the work in"
	if joined := strings.Join(said, "\n"); !strings.Contains(joined, want) {
		t.Fatalf("the run's notes = %q, want %q", said, want)
	}
	if got := conversationJournalLines(agent, want); got != 1 {
		t.Fatalf("the conversation was told %d times %q", got, want)
	}
	var row TaskNotice
	for _, kept := range agent.graph().runRows(id) {
		if kept.ID == id {
			row = kept
		}
	}
	if row.Branch != branch || row.Merge != mergeKept || len(row.Changed) != 2 || row.Copy == nil || row.Copy.Branch != branch || row.Copy.Home != "work" {
		t.Fatalf("the row = branch %q (%s), files %q, copy %+v; want the program's branch, kept, with both files", row.Branch, row.Merge, row.Changed, row.Copy)
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
	if !spec.PlainFolder || canonicalPath(spec.Workspace) != canonicalPath(folder) {
		t.Fatalf("spec = plain %v in %q, want the plain folder itself, said to be one", spec.PlainFolder, spec.Workspace)
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

// A folder with history is worked in on a branch, and the program is told
// nothing extra.
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
	if want := (delegate.Crew{Brain: "vendor/brain", Hands: "vendor/hands", Light: "vendor/light"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("crew = %+v, want %+v", got, want)
	}
	// A run the person asked a model for is handed that model beside the crew.
	asked := agent.delegateCrew(&beltRun{delegate: &program, asked: []string{"vendor/one", "vendor/two"}})
	if want := (delegate.Crew{Brain: "vendor/brain", Hands: "vendor/hands", Light: "vendor/light", Asked: []string{"vendor/one", "vendor/two"}}); !reflect.DeepEqual(asked, want) {
		t.Fatalf("asked crew = %+v, want %+v", asked, want)
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

// The tool and the prompt read the same launch registry. An empty build has no
// program the model can name; a build carrying one offers via in its real tool.
func TestProposeTaskOffersViaOnlyWhenTheLaunchCarriesAProgram(t *testing.T) {
	for _, tc := range []struct {
		name     string
		programs []delegate.Delegate
		inTask   bool
	}{
		{name: "none"},
		{name: "one", programs: testPrograms("senior-dev")},
		{name: "built-in registry", programs: builtin.All()},
		{name: "task node", programs: testPrograms("senior-dev"), inTask: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := Config{Workspace: t.TempDir(), Delegates: tc.programs, InTask: tc.inTask}
			if tc.inTask {
				config.tasker = graphForShape(t)
				config.taskID = 1
				config.taskDepth = 1
			}
			agent := &Agent{config: config}
			tools := agent.taskTools()
			if len(tools) != 1 || tools[0].Name != "propose_task" {
				t.Fatalf("task tools = %+v", tools)
			}
			_, via := schemaProperties(t, tools[0].Schema)["via"]
			wantVia := len(tc.programs) > 0 && !tc.inTask
			if via != wantVia {
				t.Fatalf("via present = %v, programs = %d: %s", via, len(tc.programs), tools[0].Schema)
			}
			page := promptWithBeltFacts(config)
			if got := strings.Contains(page, "`propose_task`'s `via`"); got != wantVia {
				t.Fatalf("prompt names via = %v, programs = %d", got, len(tc.programs))
			}
			if !wantVia && strings.Contains(page, "senior-dev") {
				t.Fatal("a launch with no program told the model about senior-dev")
			}
		})
	}
}

// CODEAF PREFERS A PROGRAM FOR THE WORK IT IS FOR, AND USES ONE WHEN ASKED.
// The paragraph said a large task "can" go to a program, and the model took a
// permission for no reason to: the owner's call of 2026-09-24 is that it
// reach for one by itself on the work the program's guide claims, over its
// own hands and its own worker, and whenever the person names one. Both
// sentences are said whatever the program lands, before the folder rule and
// apart from it, so either can be reworded without the other; and `via`
// says the same when the proposal is being written.
func TestThePagePrefersAProgramForItsWorkAndForTheAsk(t *testing.T) {
	preference := []string{
		"AND WORK A PROGRAM BUILT INTO CODEAF IS FOR GOES TO IT WHOLE, named in\n`propose_task`'s `via`",
		"rather than to you or a worker, whatever its critical path;",
		"so does work the person asks one for, by name or as `/name`.",
	}
	textOnly := testPrograms("reader")
	textOnly[0].Lands = delegate.LandsText
	for _, programs := range [][]delegate.Delegate{testPrograms("fake"), textOnly} {
		page := promptWithBeltFacts(Config{Workspace: t.TempDir(), Delegates: programs})
		for _, want := range preference {
			if !strings.Contains(page, want) {
				t.Fatalf("a build carrying %s is not told %q:\n%s", programs[0].Name, want, page)
			}
		}
		if rule := strings.Index(page, "It works in a copy"); rule >= 0 && rule < strings.Index(page, preference[2]) {
			t.Fatalf("the folder rule is said inside the preference rather than after it:\n%s", page)
		}
	}
	if !strings.Contains(taskSchemaJSON, `"via":{"type":"string","description":"A program your instructions list, to do the whole task alone in ground (or this conversation's folder): set it for work one is for, and when the person names one"}`) {
		t.Fatal("`via` does not say when it is set")
	}
}

// THE FOLDER A PROGRAM IS HANDED IS CODEAF'S TO EXPLAIN, and it is explained
// only where it is true. A program that edits files works in the proposal's
// folder itself, on a branch of its own in a repository, so the page tells the
// model to hand it the repository the work belongs in — cloned first when this
// machine lacks it — and never to brief it to work somewhere else: the failure
// this sentence was written from is senior-dev cloning a repository into the
// person's projects folder because its brief said to. A program that only
// answers reads the folder and changes nothing, so a build carrying only those
// is told nothing about branches.
func TestTheFolderRuleIsSaidWhereAProgramEditsFilesAndOnlyThere(t *testing.T) {
	tree := Config{Workspace: t.TempDir(), Delegates: testPrograms("fake")}
	page := promptWithBeltFacts(tree)
	for _, want := range []string{
		"It works in the task's folder itself, on a branch of its own in a repository, so hand\nit the repository the work belongs in",
		"clone one this machine lacks into a new folder",
		"at the commit the work names, and pass it as `ground`.",
		"Never brief it to work elsewhere.",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("a build carrying a program that edits files is not told %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "a copy of the task's folder") {
		t.Fatalf("the page still says a program works in a copy:\n%s", page)
	}
	textOnly := testPrograms("reader")
	textOnly[0].Lands = delegate.LandsText
	page = promptWithBeltFacts(Config{Workspace: t.TempDir(), Delegates: textOnly})
	if !strings.Contains(page, "- `reader`: ") {
		t.Fatalf("the program that answers is not listed:\n%s", page)
	}
	if strings.Contains(page, "task's folder itself") {
		t.Fatalf("a build whose only program reads in place is told about branches:\n%s", page)
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
	t.Setenv("CODEAF_TASK_BELT", "off")
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
