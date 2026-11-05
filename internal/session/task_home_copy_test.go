package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A task on the home folder, or on a folder holding it, reads it from a folder
// of its own. It asks nothing and copies nothing.
func TestTaskOnHomeReadsItFromAFolderOfItsOwn(t *testing.T) {
	for _, mode := range []TaskMode{TaskModeMirror, TaskModeWorktree} {
		for _, above := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/above=%v", mode, above), func(t *testing.T) {
				parent := t.TempDir()
				home := filepath.Join(parent, "person")
				private := filepath.Join(home, ".Trash", "private.txt")
				writeFile(t, private, "private")
				t.Setenv("HOME", home)
				ground := home
				if above {
					ground = parent
				}
				place := Place{Dir: t.TempDir(), Workspace: ground}
				if err := SaveMeta(place.Dir, Meta{ID: place.ID(), WorkspacePolicy: DefaultWorkspacePolicy()}); err != nil {
					t.Fatal(err)
				}
				tree, err := prepareTaskTreeOn(context.Background(), place, ground, "home-task", 1, "work", taskStand{dir: ground, mode: mode})
				if err != nil {
					t.Fatalf("a task on home must not be refused: %v", err)
				}
				if tree.mode != TaskModeReference {
					t.Fatalf("mode = %s, want reference", tree.mode)
				}
				if want := filepath.Join(canonicalPath(place.Trees()), "1"); tree.dir != want {
					t.Fatalf("dir = %s, want %s", tree.dir, want)
				}
				entries, err := os.ReadDir(tree.dir)
				if err != nil {
					t.Fatal(err)
				}
				if len(entries) != 0 {
					t.Fatalf("the task's folder must start empty, holds %d entries", len(entries))
				}
				if got := readFile(t, private); got != "private" {
					t.Fatalf("home changed: %q", got)
				}
			})
		}
	}
}

func TestGroundModeNeverMirrorsAFolderHoldingHome(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "person")
	writeFile(t, filepath.Join(home, "notes.txt"), "notes")
	t.Setenv("HOME", home)
	project := filepath.Join(home, "project")
	writeFile(t, filepath.Join(project, "notes.txt"), "notes")
	for _, tc := range []struct {
		dir   string
		owned bool
		want  TaskMode
	}{
		{home, false, TaskModeReference},
		{parent, false, TaskModeReference},
		{home, true, TaskModeReference},
		{parent, true, TaskModeReference},
		{project, false, TaskModeMirror},
		{project, true, TaskModeMirror},
	} {
		spec := taskSpec{deliverable: filepath.Join(tc.dir, "out.txt")}
		if got := groundMode(taskStand{dir: tc.dir, rung: taskGroundNamed}, spec, t.TempDir(), t.TempDir(), tc.owned); got != tc.want {
			t.Errorf("groundMode(%s, owned=%v) = %s, want %s", tc.dir, tc.owned, got, tc.want)
		}
	}
}

func TestWorkerOnHomeIsToldWhereOutputGoesAndToReportIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(t.TempDir(), "trees", "1")
	brief := composeBrief(briefWhole, "tidy my downloads list", "make the list", "", "", "", AdmissionContext{}, taskOrigin{},
		taskCopyFor(taskTree{dir: dir, ground: home, mode: TaskModeReference}))
	for _, want := range []string{home + " is yours to read", "not to change", "Everything you make goes in " + dir, "say where"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief lost %q:\n%s", want, brief)
		}
	}
	project := filepath.Join(home, "project")
	plain := composeBrief(briefWhole, "read the docs", "summarise", "", "", "", AdmissionContext{}, taskOrigin{},
		taskCopyFor(taskTree{dir: dir, ground: project, mode: TaskModeReference}))
	if strings.Contains(plain, "yours to read") {
		t.Fatalf("a reference on a project changed its brief:\n%s", plain)
	}
}

func TestTaskOnHomeNoticeNamesItsOwnFolderNotACopy(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "person")
	writeFile(t, filepath.Join(home, "notes.txt"), "notes")
	t.Setenv("HOME", home)
	place := Place{Dir: t.TempDir(), Workspace: home}
	if got, want := taskWhereNotice(place, home, 3, home, TaskModeReference), filepath.Join(place.Trees(), "3"); got != want {
		t.Fatalf("notice = %q, want %q", got, want)
	}
	// A workspace above home, asked to work "in place", is read from the task's
	// own folder too.
	if got, want := taskWhereNotice(place, parent, 4, "in place", TaskModeReference), filepath.Join(place.Trees(), "4"); got != want {
		t.Fatalf("in-place notice = %q, want %q", got, want)
	}
	if got := homeTaskRedirect(home); strings.Contains(got, "private copy") || !strings.Contains(got, "its own folder") {
		t.Fatalf("redirect says %q", got)
	}
}

// The whole road: a chat's task names the home folder as its place.
// It stands in its own folder, is told so, and the home folder is untouched.
func TestATaskNamingHomeStandsInItsOwnFolder(t *testing.T) {
	taskNamingHomeStandsInItsOwnFolder(t, true)
}

// A chat that keeps no private copies takes a model's word for where a task
// works, except that the home folder is read from the task's own folder.
func TestATaskNamingHomeInAChatWithoutCopiesStandsInItsOwnFolder(t *testing.T) {
	taskNamingHomeStandsInItsOwnFolder(t, false)
}

func taskNamingHomeStandsInItsOwnFolder(t *testing.T, owned bool) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	private := filepath.Join(home, ".Trash", "private.txt")
	writeFile(t, private, "private")
	project := filepath.Join(home, "project")
	writeFile(t, filepath.Join(project, "readme.txt"), "readme")
	place := Place{Dir: t.TempDir(), Workspace: project, Owned: owned}
	if owned {
		if err := SaveMeta(place.Dir, Meta{ID: place.ID(), WorkspacePolicy: DefaultWorkspacePolicy()}); err != nil {
			t.Fatal(err)
		}
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = project
		config.Place = place
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	world, release := heldTaskWorld(t, agent, "count.txt", "2\n")
	arguments, _ := json.Marshal(taskArguments{
		Title: "count words", Summary: "count the words in my notes",
		Brief: "Read notes.txt and write count.txt", Deliverable: "count.txt",
		Where: home, Acceptance: "count.txt holds the count",
	})
	result, isError, err := agent.proposeTask(context.Background(), arguments)
	if err != nil || isError {
		t.Fatalf("proposeTask = %q, error=%v, isError=%v", result, err, isError)
	}
	tree := <-world
	if tree.mode != TaskModeReference || !withinDir(place.Trees(), tree.dir) {
		t.Fatalf("tree = %+v, want a reference under %s", tree, place.Trees())
	}
	if _, err := os.Stat(filepath.Join(tree.dir, ".Trash")); !os.IsNotExist(err) {
		t.Fatalf("the home folder was copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "count.txt")); !os.IsNotExist(err) {
		t.Fatalf("the home folder was written: %v", err)
	}
	if got := readFile(t, private); got != "private" {
		t.Fatalf("home changed: %q", got)
	}
	if strings.Contains(result, "private copy") {
		t.Fatalf("the receipt promises a copy:\n%s", result)
	}
	node := agent.graph().node(1)
	brief := node.instructionOn(tree)
	for _, want := range []string{"yours to read", "Everything you make goes in " + tree.dir, "say where"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("worker brief lost %q:\n%s", want, brief)
		}
	}
	release()
	waitDoneNode(t, node)
}

// The default road: a run's worker is told the same thing, in the words every
// task of the run reads.
func TestARunOnHomeStandsInItsOwnFolderAndReportsWhere(t *testing.T) {
	runOnHomeReportsWhere(t, "")
}

// A place the person saved as a mirror is still read, not copied, and its
// worker is told so.
func TestARunOnASavedMirrorOfHomeIsToldWhereOutputGoes(t *testing.T) {
	runOnHomeReportsWhere(t, TaskModeMirror)
}

func runOnHomeReportsWhere(t *testing.T, saved TaskMode) {
	t.Helper()
	t.Setenv("CODEAF_TASK_BELT", "bash")
	home := t.TempDir()
	t.Setenv("HOME", home)
	private := filepath.Join(home, ".Trash", "private.txt")
	writeFile(t, private, "private")
	project := filepath.Join(home, "project")
	writeFile(t, filepath.Join(project, "readme.txt"), "readme")
	double := newBeltRunDouble("done")
	registerBeltRunEngine(t, double)
	dir := t.TempDir()
	place := Place{Dir: dir, Workspace: project, Owned: true}
	if err := SaveMeta(dir, Meta{ID: place.ID(), WorkspacePolicy: DefaultWorkspacePolicy()}); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = project
		config.Place = place
		config.SessionFile = filepath.Join(dir, placeTranscript)
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskMaxLoad = 0
		config.TaskMinFreeMB = 0
	})
	if saved != "" {
		agent.mu.Lock()
		agent.places = []PlaceRef{{Path: canonicalPath(home), Mode: string(saved), Arrival: PlaceKept}}
		agent.mu.Unlock()
	}
	arguments, _ := json.Marshal(taskArguments{
		Title: "count words", Summary: "count the words in my notes",
		Brief: "Read notes.txt and write count.txt", Deliverable: "count.txt",
		Where: home, Acceptance: "count.txt holds the count",
	})
	result, isError, err := agent.proposeTask(context.Background(), arguments)
	if err != nil || isError {
		t.Fatalf("proposeTask = %q, error=%v, isError=%v", result, err, isError)
	}
	select {
	case <-double.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not reach engine")
	}
	double.mu.Lock()
	spec := double.spec
	double.mu.Unlock()
	if !withinDir(place.Trees(), spec.Workspace) {
		t.Fatalf("run stands in %s, want under %s", spec.Workspace, place.Trees())
	}
	if _, err := os.Stat(filepath.Join(spec.Workspace, ".Trash")); !os.IsNotExist(err) {
		t.Fatalf("the home folder was copied: %v", err)
	}
	root := spec.Store.Task(spec.Store.RootID())
	doc := BeltWorkerBrief(spec.Store, root, true, false, "", "", spec.Workspace)
	for _, want := range []string{home + " is yours to read", "not to change", "assigned working directory", "say where"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("worker brief lost %q:\n%s", want, doc)
		}
	}
	close(double.release)
	<-double.finished
}

func TestTaskMirrorCopiesFolderUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	folder := filepath.Join(home, "project")
	writeFile(t, filepath.Join(folder, "notes.txt"), "project notes")
	place := Place{Dir: t.TempDir(), Workspace: folder}
	tree, err := prepareTaskTreeOn(context.Background(), place, folder, "project-copy", 1, "work", taskStand{dir: folder, mode: TaskModeMirror})
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(tree.dir, "notes.txt")); got != "project notes" {
		t.Fatalf("copied %q", got)
	}
}

func TestWorkspaceSetRefusesAFolderHoldingHome(t *testing.T) {
	above := t.TempDir()
	home := filepath.Join(above, "person")
	writeFile(t, filepath.Join(home, ".Trash", "private.txt"), "private")
	t.Setenv("HOME", home)
	t.Setenv("CODEAF_HOME", t.TempDir())
	place := Place{Dir: t.TempDir(), Workspace: above}
	for _, source := range []string{home, above} {
		request := WorkspaceSetRequest{Roots: []WorkspaceRootRequest{{Source: source}}}
		_, err := validateWorkspaceSetSources(context.Background(), place, request, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "personal home") {
			t.Fatalf("source %s: want home refusal, got %v", source, err)
		}
	}
}

func TestWorkspaceSetAdmitsAFolderInsideHome(t *testing.T) {
	home := t.TempDir()
	folder := filepath.Join(home, "project")
	writeFile(t, filepath.Join(folder, "notes.txt"), "project notes")
	t.Setenv("HOME", home)
	t.Setenv("CODEAF_HOME", t.TempDir())
	request := WorkspaceSetRequest{Roots: []WorkspaceRootRequest{{Source: folder}}}
	if _, err := validateWorkspaceSetSources(context.Background(), Place{Dir: t.TempDir(), Workspace: folder}, request, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

// A chat that refers to the home folder keeps today's behaviour: standingAim
// sends the write to the real path when no copy can be cut, so a refusal here
// would turn a kept copy into a live edit of the person's files.
func TestStandingCopyOfHomeIsNotRefused(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "notes.txt"), "notes")
	t.Setenv("HOME", home)
	agent, _, _ := standingLab(t, home)
	if _, err := agent.cutStandingTree(PlaceRef{Path: home}); err != nil {
		t.Fatalf("a standing copy must not be refused here: %v", err)
	}
}

// A place the person saved as in place is their own word, and a model naming it
// does not turn it into a reference.
func TestASavedInPlaceHomeStaysInPlaceWhenAModelNamesIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := filepath.Join(home, "project")
	writeFile(t, filepath.Join(project, "readme.txt"), "readme")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = project
		config.Place = Place{Dir: t.TempDir(), Workspace: project}
	})
	agent.mu.Lock()
	agent.places = []PlaceRef{{Path: canonicalPath(home), Mode: string(TaskModeInPlace), Arrival: PlaceSaid}}
	agent.mu.Unlock()
	stand := agent.resolveTaskGround(taskSpec{where: home, deliverable: "count.txt", acceptance: "count.txt holds the count"})
	if stand.mode != TaskModeInPlace {
		t.Fatalf("mode = %s, want the saved in place", stand.mode)
	}
}
