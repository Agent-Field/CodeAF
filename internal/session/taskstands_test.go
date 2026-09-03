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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

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

	merge, detail := tree.comeHome("write it up", []string{"notes.md"})
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

// A MODEL-FILLED `where` IS NOT A DECISION. It used to skip the ladder and
// force in-place in that directory. Ground is which folder; mode is how it
// stands; `where` answers neither.
func TestAModelWhereDoesNotStandTheWorkInPlace(t *testing.T) {
	repo := newTestRepo(t)
	request := "fix the crash"
	for _, where := range []string{"in place", ".", "./", "here", "directly", repo} {
		t.Run(where, func(t *testing.T) {
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
				config.Workspace = repo
			})
			stand := agent.resolveTaskGround(taskSpec{
				request:     request,
				where:       where,
				deliverable: "the fix",
				acceptance:  "it is fixed",
			})
			if stand.mode != TaskModeWorktree {
				t.Fatalf("mode = %q, want worktree (a model where must not skip the tree)", stand.mode)
			}
			if stand.dir != canonicalPath(repo) {
				t.Fatalf("dir = %q, want the repository so the worker is cut a worktree", stand.dir)
			}
		})
	}
}

// A PATH THEY NAMED IS GROUND, NOT AN IN-PLACE BYPASS. "fix it in ~/code/thing"
// stands ON thing, in a copy of it.
func TestAPathTheyNamedIsTheGroundNotAnInPlaceBypass(t *testing.T) {
	home := t.TempDir()
	project := newTestRepo(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = home
	})
	stand := agent.resolveTaskGround(taskSpec{
		request:     "fix the crash in " + project,
		brief:       "Repo: " + project + " — the fix belongs in shared.txt",
		deliverable: "shared.txt, changed",
		acceptance:  "the line reads differently",
		where:       project,
	})
	if stand.dir != canonicalPath(project) {
		t.Fatalf("dir = %q, want the repository they named", stand.dir)
	}
	if stand.mode != TaskModeWorktree {
		t.Fatalf("mode = %q, want worktree of the named repository, not in-place in it", stand.mode)
	}
}

// personMode is the one un-isolation this file still honors without a
// recorded Place.Mode: the door already judged their words.
func TestPersonModeUnIsolatesOnTheGroundTheLadderPicked(t *testing.T) {
	repo := newTestRepo(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = repo
	})
	stand := agent.resolveTaskGround(taskSpec{
		request:     "just edit my working copy, do not make a branch",
		personMode:  TaskModeInPlace,
		deliverable: "the fix",
		acceptance:  "it is fixed",
	})
	if stand.mode != TaskModeInPlace || stand.dir != canonicalPath(repo) {
		t.Fatalf("stand = %+v, want in-place on the ground the ladder picked", stand)
	}
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
