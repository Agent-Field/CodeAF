package session

// WHERE A TASK STANDS, end to end and at the door.
//
// The first test here is issue #76 rebuilt: a conversation opened somewhere that
// is not the project, an hour of work that was plainly about a repository three
// directories away, and every piece of machinery downstream believing the empty
// place. It is written as the whole road — a real graph, a real worktree, a real
// merge, a real check — because every part of that failure was correct on its
// own and only the seam between them was wrong.

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// heldTaskWorld lets a door test observe the worker's live directory before
// its scripted run is released. The contract is about isolation while work is
// happening, so waiting until cleanup would miss the checkout write it guards.
func heldTaskWorld(t *testing.T, agent *Agent, path, content string) (<-chan taskTree, func()) {
	t.Helper()
	world := make(chan taskTree, 1)
	release := make(chan struct{})
	var once sync.Once
	stubbedGraph(agent, func(node *TaskNode) {
		tree, ok := agent.openTaskWorld(context.Background(), node, io.Discard)
		if !ok {
			return
		}
		writeFile(t, filepath.Join(tree.dir, path), content)
		world <- tree
		<-release
		merge, changed := keptWork(tree, node.title(), []string{path})
		node.finish("scripted run ended", changed, tree.branch, merge)
		node.graph.complete(node, TaskFailed)
	})
	done := func() { once.Do(func() { close(release) }) }
	t.Cleanup(done)
	return world, done
}

// C1: a model asking for in-place repository work gets a task branch, the live
// checkout stays untouched, and the proposal receipt says why it was redirected.
func TestC1AProposalCannotPutRepositoryWorkInTheCheckout(t *testing.T) {
	repo := newTestRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = repo
		config.Place = place
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	world, release := heldTaskWorld(t, agent, "isolated.txt", "only on the task branch\n")
	arguments, _ := json.Marshal(taskArguments{
		Title: "isolate the write", Summary: "write without touching the checkout",
		Brief: "write isolated.txt", Deliverable: "isolated.txt", Where: "in place",
		Acceptance: "isolated.txt contains the line",
	})
	result, isError, err := agent.proposeTask(context.Background(), arguments)
	if err != nil || isError {
		t.Fatalf("proposeTask = %q, error=%v, isError=%v", result, err, isError)
	}
	tree := <-world
	wantSentence := whereRedirectSentence("in place", canonicalPath(repo))
	if strings.Count(result, wantSentence) != 1 {
		t.Fatalf("proposal receipt does not carry the redirect once:\n%s", result)
	}
	if tree.root != canonicalPath(repo) || !strings.HasPrefix(tree.branch, "task/") {
		t.Fatalf("tree = %+v, want a task branch of %s", tree, repo)
	}
	if !withinDir(place.Trees(), tree.dir) {
		t.Fatalf("task directory %s is not under %s", tree.dir, place.Trees())
	}
	if _, err := os.Stat(filepath.Join(repo, "isolated.txt")); !os.IsNotExist(err) {
		t.Fatalf("the live checkout was written while the worker ran: %v", err)
	}
	if list := gitOut(t, repo, "worktree", "list"); !strings.Contains(list, tree.dir) {
		t.Fatalf("git does not know the task copy:\n%s", list)
	}
	if got := taskWhereNotice(place, repo, 1, "in place", TaskModeWorktree); got == repo || got != filepath.Join(place.Trees(), "1") {
		t.Fatalf("proposal card directory = %q, want the task folder", got)
	}
	// IN PLACE RE-ENTERS THE LADDER. A contract naming no file therefore gets
	// the repository as a reference, but its redirected placement still puts
	// the worker and the proposal card in the task's own folder.
	reference := agent.resolveTaskGround(taskSpec{
		where: "in place", deliverable: "a concise answer", acceptance: "the question is answered",
	})
	if reference.mode != TaskModeReference || reference.redirect != wantSentence {
		t.Fatalf("read-only in-place request resolved as %+v, want a redirected reference", reference)
	}
	if got := taskWhereNotice(place, repo, 2, "in place", reference.mode); got != filepath.Join(place.Trees(), "2") {
		t.Fatalf("reference proposal card directory = %q, want its task folder", got)
	}
	release()
	waitDoneNode(t, agent.graph().node(1))
}

// C3: every spelling of where keeps its old plain-folder behaviour, while an
// existing or future path inside a committed repository resolves to its root.
func TestC3WhereInsideARepositoryIsBranchedAndPlainFoldersStayInPlace(t *testing.T) {
	repo := newTestRepo(t)
	inside := filepath.Join(repo, "notes")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	plain := t.TempDir()
	fresh := filepath.Join(t.TempDir(), "future", "out")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.Workspace = repo })

	for _, test := range []struct {
		name  string
		where string
		mode  TaskMode
		dir   string
		rung  string
	}{
		{"absolute path inside the repository", inside, TaskModeWorktree, canonicalPath(repo), taskGroundNamed},
		{"plain folder", plain, TaskModeInPlace, canonicalPath(plain), taskGroundNamed},
		{"future folder outside repositories", fresh, TaskModeInPlace, canonicalPath(fresh), taskGroundNamed},
		{"future relative path inside the repository", "notes/out", TaskModeWorktree, canonicalPath(repo), taskGroundNamed},
	} {
		t.Run(test.name, func(t *testing.T) {
			stand := agent.resolveTaskGround(taskSpec{where: test.where, deliverable: "out.txt", acceptance: "it exists"})
			if stand.mode != test.mode || stand.dir != test.dir || stand.rung != test.rung {
				t.Fatalf("stand = %+v, want dir %s mode %s rung %s", stand, test.dir, test.mode, test.rung)
			}
		})
	}

	place := Place{Dir: t.TempDir(), Workspace: repo}
	tree, err := prepareTaskTreeAt(context.Background(), place, repo, "named", 9, "write below notes", "notes/out", "")
	if err != nil {
		t.Fatal(err)
	}
	if tree.root != canonicalPath(repo) || tree.dir == filepath.Join(repo, "notes", "out") {
		t.Fatalf("relative repository placement made %+v", tree)
	}
	tree.releaseKept()

	created, err := prepareTaskTreeAt(context.Background(), Place{}, repo, "plain", 10, "write outside", fresh, "")
	if err != nil {
		t.Fatal(err)
	}
	if created.dir != canonicalPath(fresh) || created.merge != mergeInPlace {
		t.Fatalf("future plain folder made %+v", created)
	}
	if info, err := os.Stat(fresh); err != nil || !info.IsDir() {
		t.Fatalf("future plain folder was not created: %v", err)
	}
}

// C4: an in-place mode the person put on a referred repository remains the one
// authority that deliberately writes that repository directly.
func TestC4APersonsInPlaceModeOnAReferredRepositoryIsHonoured(t *testing.T) {
	repo := newTestRepo(t)
	workspace := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.Workspace = workspace })
	if _, err := agent.ReferPlace(repo, PlaceSaid); err != nil {
		t.Fatal(err)
	}
	if err := agent.SetPlaceMode(repo, "in place"); err != nil {
		t.Fatal(err)
	}
	stand := agent.resolveTaskGround(taskSpec{deliverable: "shared.txt", acceptance: "it changed"})
	if stand.dir != canonicalPath(repo) || stand.mode != TaskModeInPlace {
		t.Fatalf("the person's mode was overruled: %+v", stand)
	}
}

// C5: in place still means exactly that for a plain workspace and for a
// repository with no commit from which a branch could be cut.
func TestC5InPlaceWithoutACommittedRepositoryIsUnchanged(t *testing.T) {
	plain := t.TempDir()
	for _, test := range []struct {
		name      string
		workspace string
	}{
		{"plain folder", plain},
		{"repository with no commit", func() string {
			dir := t.TempDir()
			mustGit(t, dir, "init")
			return dir
		}()},
	} {
		t.Run(test.name, func(t *testing.T) {
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.Workspace = test.workspace })
			stand := agent.resolveTaskGround(taskSpec{where: "in place", deliverable: "notes.txt", acceptance: "it exists"})
			if stand.dir != canonicalPath(test.workspace) || stand.mode != TaskModeInPlace || stand.redirect != "" {
				t.Fatalf("stand = %+v, want unchanged in-place work", stand)
			}
			tree, err := prepareTaskTreeAt(context.Background(), Place{}, test.workspace, "plain", 1, "write notes", "in place", "")
			if err != nil {
				t.Fatal(err)
			}
			if tree.dir != test.workspace || tree.merge != mergeInPlace || tree.branch != "" {
				t.Fatalf("tree = %+v, want the workspace itself", tree)
			}
			note := taskNote(TaskNotice{ID: 1, Title: "Write notes", State: TaskDone, Merge: mergeInPlace}, "", TaskSettleAsk, landingAddress{person: true})
			if !strings.Contains(note, "there was no repository to branch") {
				t.Fatalf("in-place completion says nothing about the missing branch:\n%s", note)
			}
		})
	}
}

// THE DEFECT: the chat was opened in a home directory, the harness cut both
// workers a worktree from an empty repository beside the session, and the check
// that decides whether work is finished ran in that empty tree — so two tasks
// with open, correct pull requests behind them landed as incomplete.
//
// What the conversation had actually been doing was reading the project. That is
// the evidence the ground is resolved from, and this test asserts every place
// the answer has to reach: the branch is cut from the PROJECT, the record says
// so, the work merges into the PROJECT's own branch, and the auditor stands in a
// clean copy of the project rather than in a directory holding nothing.
func TestATaskStandsWhereTheConversationHasBeenWorking(t *testing.T) {
	home := newTestRepo(t)
	project := newTestRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := &routedCompleter{
		parent: []step{
			// The conversation reads the project — one ordinary call, and the whole
			// of what the ladder has to go on.
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("call-read", "read",
					`{"path":`+quoteJSON(filepath.Join(project, "shared.txt"))+`}`), nil
			},
			proposeCall("Add the greeting", "write hello.txt containing hi"),
			finalText("handed off"),
		},
		child: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("call-write", "write", `{"path":"hello.txt","content":"hi\n"}`), nil
			},
			finalText("Wrote hello.txt with the greeting."),
		},
		// THE CHECK STANDS WHERE THE WORK STOOD. The auditor reads a file that
		// exists only in the project, so a restore cut from the conversation's own
		// empty repository can only answer REFUTED.
		audit: []step{
			bashCall("call-cat", "cat shared.txt"),
			verdictFromEvidence("the original line",
				"VERIFIED — the restore holds the project and the new file",
				"REFUTED — the restore does not hold the project"),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = home
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	graph := agent.graph()

	events, err := agent.Submit(context.Background(), "add a greeting to the project")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	node := graph.node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	waitDoneNode(t, node)
	notice := node.notice()

	if notice.State != TaskDone {
		t.Fatalf("state = %q, report = %q", notice.State, notice.Report)
	}
	if want := canonicalPath(project); notice.Ground != want {
		t.Fatalf("ground = %q, want the project the conversation was reading %q", notice.Ground, want)
	}
	if notice.Mode != TaskModeWorktree {
		t.Fatalf("mode = %q, want %q", notice.Mode, TaskModeWorktree)
	}
	if notice.Merge != mergeMerged {
		t.Fatalf("merge = %q, report = %q", notice.Merge, notice.Report)
	}
	if content := readFile(t, filepath.Join(project, "hello.txt")); content != "hi\n" {
		t.Fatalf("the work did not land in the project: %q", content)
	}
	if _, err := os.Stat(filepath.Join(home, "hello.txt")); !os.IsNotExist(err) {
		t.Fatal("the work landed in the conversation's own repository")
	}
	// AND THE RECORD CARRIES IT, so a resumed node finds its own branch again and
	// a row in the project's history says which project it was.
	graph.mu.Lock()
	record := node.recordLocked()
	graph.mu.Unlock()
	if record.Ground != canonicalPath(project) || record.Mode != TaskModeWorktree {
		t.Fatalf("the checkpoint records ground %q mode %q", record.Ground, record.Mode)
	}
}

// A path in the contract that is outside the ground and in no repository is
// refused in one sentence. The work would have nowhere to put what it made, and
// starting it to have a guard turn every write back is a worse answer than
// saying so before anybody spends anything.
func TestATaskNamingAFolderItDoesNotStandInIsRefused(t *testing.T) {
	repo := newTestRepo(t)
	elsewhere := filepath.Join(t.TempDir(), "somewhere-else")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = repo
	})

	arguments, _ := json.Marshal(taskArguments{
		Title: "write the notes", Summary: "s", Brief: "b",
		Deliverable: "a file at " + filepath.Join(elsewhere, "notes.md"),
		Acceptance:  "the file is there",
	})
	result, isError, err := agent.proposeTask(context.Background(), arguments)
	if err != nil {
		t.Fatalf("proposeTask errored the turn: %v", err)
	}
	if !isError {
		t.Fatalf("a task pointed outside its ground was admitted: %q", result)
	}
	if !strings.HasPrefix(result, "this task names a folder it does not stand in: ") {
		t.Fatalf("the refusal reads %q", result)
	}
	graph := agent.graph()
	graph.mu.Lock()
	admitted := len(graph.nodes)
	graph.mu.Unlock()
	if admitted != 0 {
		t.Fatalf("the graph admitted %d nodes for a refused proposal", admitted)
	}
}

// TWO PLACES WITH REAL WEIGHT ARE A QUESTION. A conversation that has been in two
// repositories has not said which one this work is for, and the one thing the
// harness may not do is pick — an hour of work in the wrong project is what a
// coin toss costs.
func TestAConversationInTwoPlacesIsAskedWhichOne(t *testing.T) {
	first := newTestRepo(t)
	second := newTestRepo(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	// The conversation's own record of where it has been: one read in each
	// project, which is exactly the evidence that settles nothing.
	agent.mu.Lock()
	agent.messages = append(agent.messages,
		readCallMessage("call-a", filepath.Join(first, "shared.txt")),
		readCallMessage("call-b", filepath.Join(second, "shared.txt")))
	agent.mu.Unlock()

	arguments, _ := json.Marshal(taskArguments{
		Title: "fix the crash", Summary: "s", Brief: "b",
		Deliverable: "the fix", Acceptance: "the tests pass",
	})
	result, isError, err := agent.proposeTask(context.Background(), arguments)
	if err != nil {
		t.Fatalf("proposeTask errored the turn: %v", err)
	}
	if !isError {
		t.Fatalf("an ambiguous ground was guessed at: %q", result)
	}
	for _, want := range []string{
		"this conversation has been working in two places",
		canonicalPath(first), canonicalPath(second), "Ask the person",
	} {
		if !strings.Contains(result, want) {
			t.Fatalf("the question does not say %q: %q", want, result)
		}
	}
}

// A PLAIN FOLDER IS MIRRORED AND LANDS BY NAME. There is no history to branch
// from, so the isolation a repository gets for free is made by copying — and
// what comes home is what the node wrote and nothing else it left behind.
func TestAFolderGroundIsMirroredAndLandsByName(t *testing.T) {
	ground := t.TempDir()
	writeFile(t, filepath.Join(ground, "notes.md"), "the original line\n")
	writeFile(t, filepath.Join(ground, "deep", "under.txt"), "kept\n")

	tree, err := prepareTaskTreeOn(context.Background(), Place{}, t.TempDir(), "aaaa1111aaaa1111", 3, "write it up",
		taskStand{dir: ground, mode: TaskModeMirror})
	if err != nil {
		t.Fatalf("prepareTaskTreeOn: %v", err)
	}
	if tree.dir == ground {
		t.Fatal("a mirrored task was put in the folder it was supposed to be a copy of")
	}
	if got := readFile(t, filepath.Join(tree.dir, "deep", "under.txt")); got != "kept\n" {
		t.Fatalf("the mirror does not hold the folder: %q", got)
	}
	// What the node does: one file changed, one left behind that nobody wrote.
	writeFile(t, filepath.Join(tree.dir, "notes.md"), "the written line\n")
	writeFile(t, filepath.Join(tree.dir, "build.log"), "noise\n")

	merge, detail, _ := tree.comeHome("write it up", []string{"notes.md"})
	if merge != mergeInPlace || detail != "" {
		t.Fatalf("the mirror landed as %q: %s", merge, detail)
	}
	if got := readFile(t, filepath.Join(ground, "notes.md")); got != "the written line\n" {
		t.Fatalf("the work did not come home: %q", got)
	}
	if _, err := os.Stat(filepath.Join(ground, "build.log")); !os.IsNotExist(err) {
		t.Fatal("the landing carried something nobody wrote")
	}
}

// AND THE CHECK ON A MIRROR IS A COPY OF THE FOLDER ITSELF. The node worked in a
// copy, so the original is sitting there untouched — a restore read off the
// node's own directory by timestamp would call the whole mirror "left behind"
// and hand the auditor an empty tree, which is issue #76's third defect wearing
// different clothes.
func TestTheCheckOnAMirrorIsCutFromTheFolderItStandsOn(t *testing.T) {
	ground := t.TempDir()
	writeFile(t, filepath.Join(ground, "notes.md"), "the original line\n")

	tree, err := prepareTaskTreeOn(context.Background(), Place{}, t.TempDir(), "bbbb2222bbbb2222", 4, "write it up",
		taskStand{dir: ground, mode: TaskModeMirror})
	if err != nil {
		t.Fatalf("prepareTaskTreeOn: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "notes.md"), "the written line\n")

	restored, why := restoreTaskWork(nil, tree, []string{"notes.md"})
	if !restored.restored {
		t.Fatalf("no restore was made: %s", why)
	}
	defer restored.drop()
	if got := readFile(t, filepath.Join(restored.dir, "notes.md")); got != "the written line\n" {
		t.Fatalf("the restore does not hold what the work wrote: %q", got)
	}
}

// The ladder itself, rung by rung, with no graph behind it.
func TestTheGroundLadderClimbsInOrder(t *testing.T) {
	repo := newTestRepo(t)
	plain := t.TempDir()

	t.Run("said outranks everything", func(t *testing.T) {
		agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.Workspace = plain })
		stand := agent.resolveTaskGround(taskSpec{ground: repo, deliverable: "shared.txt", acceptance: "it changed"})
		if stand.dir != canonicalPath(repo) || stand.rung != taskGroundSaid {
			t.Fatalf("stand = %+v", stand)
		}
		if stand.mode != TaskModeWorktree {
			t.Fatalf("mode = %q, want a branch off the repository it was told about", stand.mode)
		}
	})

	t.Run("standing in is what it always was", func(t *testing.T) {
		agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.Workspace = repo })
		stand := agent.resolveTaskGround(taskSpec{deliverable: "an answer", acceptance: "it is written"})
		if stand.dir != canonicalPath(repo) || stand.rung != taskGroundStandingIn {
			t.Fatalf("stand = %+v", stand)
		}
		// A conversation standing in its own project keeps its branch whatever the
		// deliverable looks like: this design came to place work that was going
		// somewhere wrong, not to take a worktree off work that was going right.
		if stand.mode != TaskModeWorktree {
			t.Fatalf("mode = %q, want %q", stand.mode, TaskModeWorktree)
		}
	})

	t.Run("nothing is the conversation's own folder", func(t *testing.T) {
		agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.Workspace = plain })
		stand := agent.resolveTaskGround(taskSpec{deliverable: "an answer", acceptance: "it is written"})
		if stand.dir != canonicalPath(plain) || stand.rung != taskGroundNothing {
			t.Fatalf("stand = %+v", stand)
		}
		if stand.mode != TaskModeFolder {
			t.Fatalf("mode = %q, want %q", stand.mode, TaskModeFolder)
		}
	})

	t.Run("a repository the work only reads is not branched", func(t *testing.T) {
		agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.Workspace = plain })
		stand := agent.resolveTaskGround(taskSpec{
			ground:      repo,
			deliverable: "an answer in this conversation, naming the three worst offenders",
			acceptance:  "the three are named",
		})
		if stand.mode != TaskModeReference {
			t.Fatalf("mode = %q, want %q for work that writes nothing under the repository", stand.mode, TaskModeReference)
		}
	})

	t.Run("a part stands where its parent stands", func(t *testing.T) {
		elsewhere := newTestRepo(t)
		agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.Workspace = repo })
		// Everything that could move a root proposal — a brief naming another
		// repository, a conversation that has been reading one — is ignored for a
		// part, because a part's branch is cut from its parent's worktree and merges
		// back into it.
		agent.mu.Lock()
		agent.messages = append(agent.messages, readCallMessage("call-a", filepath.Join(elsewhere, "shared.txt")))
		agent.mu.Unlock()
		stand := agent.resolveTaskGround(taskSpec{
			parent: 3, depth: 2,
			brief:       "the fix belongs in " + filepath.Join(elsewhere, "shared.txt"),
			deliverable: "shared.txt, changed",
			acceptance:  "the line reads differently",
		})
		if stand.dir != canonicalPath(repo) || stand.rung != taskGroundStandingIn {
			t.Fatalf("a part was re-grounded away from its parent: %+v", stand)
		}
	})

	t.Run("the brief re-grounds when it knows better", func(t *testing.T) {
		agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.Workspace = plain })
		stand := agent.resolveTaskGround(taskSpec{
			brief:       "Repo: " + repo + " — the fix belongs in shared.txt",
			deliverable: "shared.txt, changed",
			acceptance:  "the line reads differently",
		})
		if stand.dir != canonicalPath(repo) {
			t.Fatalf("the brief named a repository and the ground stayed at %q", stand.dir)
		}
	})
}

// readCallMessage is one assistant turn that read one path: the shape the
// touched rung reads the conversation for.
func readCallMessage(id, path string) ai.Message {
	return ai.Message{
		Role: "assistant",
		ToolCalls: []ai.ToolCall{{
			ID:       id,
			Type:     "function",
			Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":` + quoteJSON(path) + `}`},
		}},
	}
}
