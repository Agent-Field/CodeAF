package session

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
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
// the run's result, and that commit comes home to the folder the copy was cut
// from. The engine is a double whose `work` hook plays the program: two files,
// two commits, the way senior-dev commits every edit.
func TestADelegatedRunSquashesTheProgramsCommitsIntoOneAndLandsIt(t *testing.T) {
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
	endBeltRun(t, agent, double)

	// ONE COMMIT ABOVE THE BASE, and it is codeaf's landing commit, not the
	// program's two.
	log := gitOut(t, conversation, "log", "--format=%s%n%b", base+"..HEAD")
	if strings.Contains(log, "wip(edit)") {
		t.Fatalf("the program's own commits reached the branch:\n%s", log)
	}
	subjects := strings.TrimSpace(gitOut(t, conversation, "log", "--format=%s", base+"..HEAD"))
	lines := strings.Split(subjects, "\n")
	// A merge may add its own commit above the squash; the squash itself is
	// exactly one, and it is the task's title.
	found := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "task: ") {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("want exactly one `task:` commit above the base, got %d in:\n%s", found, subjects)
	}
	if !strings.Contains(log, "fake's model said: tests pass") {
		t.Fatalf("the landing commit's body does not carry the run's result:\n%s", log)
	}
	for _, name := range []string{"one.txt", "two.txt"} {
		if _, err := os.Stat(filepath.Join(conversation, name)); err != nil {
			t.Fatalf("%s did not come home: %v", name, err)
		}
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
		"It works in a copy of the task's folder and only that copy lands",
		"clone one this machine lacks into a new folder",
		"branch at the commit the work names, and pass it as `ground`.",
		"Never brief it to work\nelsewhere.",
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
