package session

// THE HOLD A PROGRAM'S RUN HAS ON ITS FOLDER (programhold.go), in real folders
// and real git: a folder is busy when a held folder is it, holds it or is
// inside it, and a folder nobody holds is left exactly as it was by every
// door that asks.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/approval"
)

// holdFolder readies dir for a run of the fake program and answers the
// folder, held until the test ends.
func holdFolder(t *testing.T, dir, title string) *ProgramFolder {
	t.Helper()
	folder, err := PrepareProgramFolder(ProgramFolderOrder{Program: testPrograms("fake")[0], Dir: dir, Title: title, Holder: "task 4 (" + title + ")", Keep: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { folder.Finish("") })
	return folder
}

// prepareErr is the refusal a run readied on dir meets, "" when it was let
// through (and then finished at once).
func prepareErr(t *testing.T, dir string) string {
	t.Helper()
	folder, err := PrepareProgramFolder(ProgramFolderOrder{Program: testPrograms("fake")[0], Dir: dir, Title: "The second run", Holder: "task 5 (The second run)", Keep: t.TempDir()})
	if err != nil {
		return err.Error()
	}
	folder.Finish("")
	return ""
}

// ONE FOLDER TAKES ONE PROGRAM RUN, AND SO DO THE FOLDERS INSIDE IT. A run on a
// plain folder of projects puts back whatever changed under it once it has
// submitted, so a second run in one of those projects had its work reverted
// under it while each held only its own exact path. A run on a folder inside a
// held one, or around one, is refused naming the run and the folder it holds,
// at its start and at its card alike; two runs side by side are not.
func TestAProgramRunIsRefusedAFolderInsideOrAroundAHeldOne(t *testing.T) {
	work := t.TempDir()
	project := filepath.Join(work, "proj")
	sibling := filepath.Join(work, "other")
	for _, dir := range []string{project, sibling} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("inside", func(t *testing.T) {
		held := holdFolder(t, work, "Tidy the projects")
		want := project + " is busy: fake, task 4 (Tidy the projects), is working in " + canonicalPath(work) +
			", which holds it, and one folder takes one program run at a time; ask again when that run has ended"
		if got := prepareErr(t, project); got != want {
			t.Fatalf("a run inside a held folder = %q, want %q", got, want)
		}
		folder := project
		if got := programGroundRefusal(testPrograms("fake")[0], &folder); got != want {
			t.Fatalf("its card = %q, want %q", got, want)
		}
		held.Finish("")
		if got := prepareErr(t, project); got != "" {
			t.Fatalf("the folder was still refused after the run around it ended: %q", got)
		}
	})
	t.Run("around", func(t *testing.T) {
		held := holdFolder(t, project, "Fix the parser")
		want := work + " is busy: fake, task 4 (Fix the parser), is working in " + canonicalPath(project) +
			", which is inside it, and one folder takes one program run at a time; ask again when that run has ended"
		if got := prepareErr(t, work); got != want {
			t.Fatalf("a run around a held folder = %q, want %q", got, want)
		}
		if got := prepareErr(t, sibling); got != "" {
			t.Fatalf("a run beside a held folder was refused: %q", got)
		}
		held.Finish("")
	})
	t.Run("a repository inside", func(t *testing.T) {
		repo := newTestRepo(t)
		parent := filepath.Dir(repo)
		held := holdFolder(t, parent, "Tidy the projects")
		got := prepareErr(t, repo)
		if !strings.HasPrefix(got, repo+" is busy: fake, task 4 (Tidy the projects), is working in "+canonicalPath(parent)+", which holds it") {
			t.Fatalf("a repository inside a held folder = %q", got)
		}
		if head := currentBranch(repo); head != "work" {
			t.Fatalf("a refused repository was switched to %q", head)
		}
		held.Finish("")
	})
}

// holdOf is the hold a refusal names for a run holdFolder readied on dir.
func holdOf(dir, title string) programHold {
	return programHold{dir: canonicalPath(dir), holder: "fake, task 4 (" + title + ")"}
}

// THE CHAT'S OWN FILE TOOLS DO NOT WRITE IN A FOLDER A PROGRAM'S RUN HOLDS.
// senior-dev works in the person's folder itself, and once it has submitted it
// puts back whatever changed there and removes whatever was added; a file the
// chat wrote meanwhile was deleted with no copy kept, while the chat had told
// the person it was written. Every hand that puts a file at a path it names is
// refused, through the real tool door, naming the file, the folder and the
// run; reading stays open, a path outside the folder is written, and the same
// write goes through once the run has ended.
func TestTheChatsFileToolsAreRefusedAFolderAProgramHolds(t *testing.T) {
	repo := newTestRepo(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = repo
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
	})
	held := holdFolder(t, repo, "Fix the parser")
	result := agent.executeTool(context.Background(), agent.newEpisode(), nil,
		withdrawnCall("c1", "write", `{"path":"NOTES.md","content":"the chat's note\n"}`), "")
	if want := programHoldWriteRefusal("NOTES.md", holdOf(repo, "Fix the parser")); !result.isError || result.text != want {
		t.Fatalf("the chat's write = %q (error %v), want %q", result.text, result.isError, want)
	}
	if _, err := os.Stat(filepath.Join(repo, "NOTES.md")); !os.IsNotExist(err) {
		t.Fatalf("the refused write was written: %v", err)
	}
	if read := agent.executeTool(context.Background(), agent.newEpisode(), nil,
		withdrawnCall("c2", "read", `{"path":"shared.txt"}`), ""); read.isError {
		t.Fatalf("reading in a held folder was refused: %q", read.text)
	}
	guard := programHoldGuard{agent: agent}
	for _, call := range []ai.ToolCall{
		scopedCall("edit", filepath.Join(repo, "shared.txt")),
		withdrawnCall("c3", "edit_video", `{"action":"join","path":"cut.mp4"}`),
		withdrawnCall("c4", "generate_image", `{"prompt":"a harbour","path":"art/harbour"}`),
		withdrawnCall("c5", "speak", `{"text":"hello","path":"clips/hello"}`),
		withdrawnCall("c6", "generate_music", `{"description":"a tune","path":"tune"}`),
		withdrawnCall("c7", "generate_video", `{"prompt":"a boat","path":"boat"}`),
		withdrawnCall("c12", "generate_image", `{"prompt":"a harbour"}`),
		withdrawnCall("c13", "workspace_restore", `{"snapshot":"before","confirm":true}`),
		withdrawnCall("c14", "workspace_merge", `{"fork":"try-it"}`),
		withdrawnCall("c18", "edit_video", `{"action":"join"}`),
	} {
		if _, refusal, ok := guard.PreAction(context.Background(), nil, nil, call); ok || !refusal.isError || !strings.Contains(refusal.text, "where fake, task 4 (Fix the parser), is working, so nothing was written") {
			t.Fatalf("%s into the held folder = %+v (let through %v)", call.Function.Name, refusal, ok)
		}
	}
	elsewhere := t.TempDir()
	for _, call := range []ai.ToolCall{
		scopedCall("write", filepath.Join(elsewhere, "notes.md")),
		withdrawnCall("c9", "edit_video", `{"action":"measure","path":"cut.mp4"}`),
		withdrawnCall("c10", "bash", `{"command":"echo hi > note.txt"}`),
	} {
		if _, refusal, ok := guard.PreAction(context.Background(), nil, nil, call); !ok {
			t.Fatalf("%s was refused though it writes nothing in the held folder: %q", call.Function.Name, refusal.text)
		}
	}
	held.Finish("")
	for _, call := range []ai.ToolCall{
		withdrawnCall("c15", "workspace_restore", `{"snapshot":"before","confirm":true}`),
		withdrawnCall("c16", "workspace_merge", `{"fork":"try-it"}`),
		withdrawnCall("c17", "generate_image", `{"prompt":"a harbour"}`),
	} {
		if _, refusal, ok := guard.PreAction(context.Background(), nil, nil, call); !ok {
			t.Fatalf("%s was refused after the hold ended: %q", call.Function.Name, refusal.text)
		}
	}
	if again := agent.executeTool(context.Background(), agent.newEpisode(), nil,
		withdrawnCall("c11", "write", `{"path":"NOTES.md","content":"the chat's note\n"}`), ""); again.isError {
		t.Fatalf("the write was still refused after the run ended: %q", again.text)
	}
}

// Every registered chat hand has an explicit disposition at a folder hold.
// Bash is deliberately open because its command's effects cannot be known
// from its arguments; task tools have their separate folder admission guard.
func TestProgramHoldClassifiesEveryRegisteredChatTool(t *testing.T) {
	workspace := t.TempDir()
	agent := &Agent{config: Config{Workspace: workspace}}
	fenced := map[string]bool{
		"write": true, "edit": true, "edit_video": true,
		"generate_image": true, "generate_music": true, "generate_video": true, "speak": true,
		"workspace_restore": true, "workspace_merge": true,
	}
	// These verbs read, write codeaf's state outside the held folder, or start
	// work whose own admission guard refuses a held folder. Bash is the one
	// unguarded file writer because a shell command has no knowable path set.
	notFolderWrites := map[string]bool{
		"bash": true, "read": true, "ls": true, "find": true, "grep": true,
		"manual": true, "ask": true, "jobs": true, "watch": true,
		"track": true, "commit": true, "recall": true, "remember": true, "forget": true,
		"propose_task": true, "tasks": true, "quick_task": true,
		"use_skill": true, "load_capability": true, "view_image": true,
		"read_document": true, "search_conversations": true,
		"workspace": true, "workspace_snapshots": true, "workspace_fork": true,
		"services": true, "use_service": true,
		"settings": true, "change_setting": true,
		"stand": true, "items": true, "revise_assignment": true, "divide_work": true,
		"build_harness": true, "list_harnesses": true, "list_subharnesses": true,
		"propose_subharness": true, "revise_design": true,
		"web_search": true, "web_fetch": true,
		"gmail_read": true, "gmail_search": true, "gmail_send": true,
		"calendar_list": true, "calendar_create": true,
		"slack_search": true, "slack_read_thread": true, "slack_send": true, "slack_list_channels": true,
	}
	for name := range universeToolNames(t) {
		if !fenced[name] && !notFolderWrites[name] {
			t.Errorf("registered tool %q has no folder hold disposition", name)
		}
	}
	for _, name := range []string{"workspace_restore", "workspace_merge", "generate_image"} {
		if !fenced[name] {
			t.Fatalf("%s lost its folder hold", name)
		}
	}
	for _, call := range []ai.ToolCall{
		withdrawnCall("h1", "write", `{"path":"file.txt","content":"x"}`),
		withdrawnCall("h2", "edit", `{"path":"file.txt","old":"x","new":"y"}`),
		withdrawnCall("h3", "edit_video", `{"action":"join","path":"cut.mp4"}`),
		withdrawnCall("h10", "edit_video", `{"action":"join"}`),
		withdrawnCall("h4", "generate_image", `{"prompt":"a harbour"}`),
		withdrawnCall("h5", "generate_music", `{"description":"a song","path":"song.mp3"}`),
		withdrawnCall("h6", "generate_video", `{"prompt":"a boat","path":"boat.mp4"}`),
		withdrawnCall("h7", "speak", `{"text":"hello","path":"voice.wav"}`),
		withdrawnCall("h8", "workspace_restore", `{"snapshot":"before","confirm":true}`),
		withdrawnCall("h9", "workspace_merge", `{"fork":"try-it"}`),
	} {
		path, _, ok := agent.savingPath(call)
		if !ok || !strings.HasPrefix(canonicalPath(path), canonicalPath(workspace)+string(filepath.Separator)) && canonicalPath(path) != canonicalPath(workspace) {
			t.Errorf("%s has no path inside the workspace for its folder hold: %q, %v", call.Function.Name, path, ok)
		}
	}
}

// AN ORDINARY TASK IS REFUSED A FOLDER A PROGRAM'S RUN HOLDS, OR ONE AROUND
// IT, BEFORE IT STARTS. A task cut from the held repository recorded the
// program's branch as the person's, sealed its unfinished edits in as theirs,
// and merged back into the live checkout under it. Every door says so in one
// sentence: a proposal before its card, a typed `/task`, a quick task, a node
// starting on the session's own graph, and a run on the run road. A reference,
// which only reads its ground, is not refused, and a folder nobody holds is
// untouched by any of it.
func TestAnOrdinaryTaskIsRefusedAFolderAProgramHolds(t *testing.T) {
	repo := newTestRepo(t)
	registerBeltRunEngine(t, newBeltRunDouble("done"))
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = repo
		config.Place = Place{Dir: t.TempDir()}
		config.AskConsent = false
	})
	spec := taskSpec{ground: repo, brief: "fix the parser", deliverable: "the parser fixed in parser.go", acceptance: "its tests pass"}
	if stand := agent.resolveTaskGround(spec); stand.refusal != "" || standHeldRefusal(stand, repo) != "" {
		t.Fatalf("a folder nobody holds was refused: %+v", stand)
	}
	holdFolder(t, repo, "Fix the parser")
	want := repo + " is busy: fake, task 4 (Fix the parser), is working in it, and nothing else of codeaf's works there until that run has ended; wait for it, or stop it, then ask again"
	if stand := agent.resolveTaskGround(spec); stand.refusal != canonicalPath(repo)+strings.TrimPrefix(want, repo) {
		t.Fatalf("the proposal's refusal = %q, want %q", stand.refusal, want)
	}
	if _, _, _, err := agent.StartTask(context.Background(), "fix the parser", false); err == nil || !strings.Contains(err.Error(), " is busy: fake, task 4 (Fix the parser), is working in it, and nothing else of codeaf's works there") {
		t.Fatalf("a typed /task = %v, want it refused", err)
	}
	if _, _, refusal := agent.admitQuick(quickAsk{line: "tidy the readme"}); refusal.said != want {
		t.Fatalf("a quick task = %q, want %q", refusal.said, want)
	}
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	node := &TaskNode{graph: graph, id: 9, Ground: repo, Mode: TaskModeWorktree}
	if _, err := prepareTaskTreeForNode(context.Background(), agent.config.Place, repo, "a1a1a1a1a1a1a1a1", node); err == nil || err.Error() != want {
		t.Fatalf("a node starting there = %v, want %q", err, want)
	}
	if err := agent.startKnownTaskRun(context.Background(), agent.graph().reserve(), "Fix", "fix the parser", nil, taskStand{dir: repo, mode: TaskModeWorktree}, ""); err == nil || err.Error() != want {
		t.Fatalf("a run on the run road = %v, want %q", err, want)
	}
	if branches := strings.TrimSpace(gitOut(t, repo, "worktree", "list", "--porcelain")); strings.Count(branches, "worktree ") != 1 {
		t.Fatalf("a refused task cut a working copy from the held repository:\n%s", branches)
	}
	if refusal := standHeldRefusal(taskStand{dir: repo, mode: TaskModeReference}, repo); refusal != "" {
		t.Fatalf("a reference, which only reads, was refused: %q", refusal)
	}
	around := filepath.Dir(repo)
	if refusal := programHoldRefusal(around); !strings.Contains(refusal, "is working in "+canonicalPath(repo)+", which is inside it") {
		t.Fatalf("a task on a folder around the held one = %q", refusal)
	}
}

// A TASK THAT WAS ALREADY RUNNING LANDS BESIDE A HELD FOLDER, NOT INTO IT. Its
// branch is kept rather than merged into the program's live checkout — where
// the program's restore would have undone it while its row said it landed —
// a mirror is not laid, and a `/land` of the chat's own copy is refused with
// the copy left whole; each goes through once the run has ended.
func TestATaskThatWasRunningLandsBesideAHeldFolder(t *testing.T) {
	t.Run("a branch", func(t *testing.T) {
		repo := newTestRepo(t)
		tree, err := prepareTaskTree(Place{Dir: t.TempDir()}, repo, "b1b1b1b1b1b1b1b1", 1, "update the changelog")
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(tree.dir, "CHANGELOG.md"), "a line\n")
		held := holdFolder(t, repo, "Fix the parser")
		programTip := strings.TrimSpace(gitOut(t, repo, "rev-parse", held.Branch))
		merge, said, _, _ := tree.comeHome("update the changelog", []string{"CHANGELOG.md"}, gitSignature{})
		if merge != mergeKept || !strings.Contains(said, "its branch "+tree.branch+" was kept: fake, task 4 (Fix the parser), is working in it — bring it in when that run has ended") {
			t.Fatalf("the landing = %q %q, want its branch kept beside the held folder", merge, said)
		}
		if tip := strings.TrimSpace(gitOut(t, repo, "rev-parse", held.Branch)); tip != programTip {
			t.Fatalf("the task merged into the program's branch: %s, want %s", tip, programTip)
		}
		if files := gitOut(t, repo, "ls-tree", "--name-only", tree.branch); !strings.Contains(files, "CHANGELOG.md") {
			t.Fatalf("the kept branch does not hold the work:\n%s", files)
		}
	})
	t.Run("a mirror", func(t *testing.T) {
		plain, copy := t.TempDir(), t.TempDir()
		writeFile(t, filepath.Join(copy, "notes.md"), "the family's notes\n")
		held := holdFolder(t, plain, "Fix the parser")
		mirror := taskTree{dir: copy, ground: plain, mode: TaskModeMirror}
		merge, said, _, refusal := mirror.comeHome("notes", []string{"notes.md"}, gitSignature{})
		if merge != mergeAborted || refusal != refusedByTheWork || !strings.HasPrefix(said, "its work was not laid into "+plain+" and is kept in "+copy+": ") {
			t.Fatalf("the mirror's landing = %q %q %v, want it kept in its copy", merge, said, refusal)
		}
		if _, err := os.Stat(filepath.Join(plain, "notes.md")); !os.IsNotExist(err) {
			t.Fatalf("the mirror was laid into the held folder: %v", err)
		}
		held.Finish("")
		if merge, said, _, _ := mirror.comeHome("notes", []string{"notes.md"}, gitSignature{}); merge != mergeInPlace {
			t.Fatalf("the mirror's landing once the run ended = %q %q", merge, said)
		}
	})
	t.Run("a /land", func(t *testing.T) {
		repo := newTestRepo(t)
		agent, _, _ := standingLab(t, repo)
		writeThrough(t, agent, filepath.Join(repo, "shared.txt"), "the changed line\n")
		held := holdFolder(t, repo, "Fix the parser")
		if _, err := agent.Land(repo); err == nil || !strings.Contains(err.Error(), " is busy: fake, task 4 (Fix the parser), is working in it") {
			t.Fatalf("a /land into the held folder = %v", err)
		}
		if waiting := agent.UnlandedChanges(); len(waiting) != 1 {
			t.Fatalf("the refused landing dropped the copy: %+v", waiting)
		}
		held.Finish("")
		if landing, err := agent.Land(repo); err != nil || landing.Merged != mergeMerged {
			t.Fatalf("the /land once the run ended = %+v, %v", landing, err)
		}
	})
}
