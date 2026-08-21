package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── ASKING ABOUT THE WHOLE MACHINE ──────────────────────────────────────────
//
// One terminal now holds several projects at once, so "what is running outside
// this conversation" stopped being a question about a directory. These are the
// whole of what a session is allowed to say about the rest of the machine, and
// the first of them is the one that matters most: that nothing changed for
// anybody who did not ask.

// machineSession lays one conversation down under a places root exactly as a
// live session leaves it: a transcript so the folder is a session at all, a
// meta.json naming it and its workspace, and a presence file dated age ago.
func machineSession(t *testing.T, root, bucket, id, title, workspace string, pid int, age time.Duration, tasks ...PresenceTask) string {
	t.Helper()
	dir := filepath.Join(root, bucket, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("session folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, placeTranscript), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	meta, err := json.Marshal(Meta{
		ID:         id,
		Title:      title,
		Workspace:  workspace,
		LastUserAt: time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("marshal meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, placeMeta), meta, 0o600); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	presence, err := json.Marshal(SessionPresence{
		Schema:       presenceSchema,
		SessionID:    id,
		Workspace:    workspace,
		PID:          pid,
		UpdatedAt:    time.Now().Add(-age),
		State:        PresenceWorking,
		RunningTasks: tasks,
	})
	if err != nil {
		t.Fatalf("marshal presence: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, presenceName), presence, 0o600); err != nil {
		t.Fatalf("write presence: %v", err)
	}
	return dir
}

// machine builds a root holding this session's own project and two others: one
// with work in flight, one quiet. It answers the caller's own session folder.
func machine(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mine := machineSession(t, root, "here", "mine", "this chat", "/work/aforge", os.Getpid(), time.Second)
	machineSession(t, root, "wisp", "theirs", "parser work", "/Users/ada/code/wisp", 4242, time.Second,
		PresenceTask{ID: "4", Title: "Port the parser", State: string(TaskRunning),
			StartedAt: time.Now().Add(-2 * time.Minute),
			Files:     []string{"internal/parse/lex.go"}})
	machineSession(t, root, "quiet", "idle", "nothing doing", "/Users/ada/code/quiet", 4242, time.Second)
	return mine
}

// THE DEFAULT IS THE OLD BEHAVIOUR, EXACTLY. A model that has never heard of
// this argument must get the answer it always got — no other project's name,
// no heading over it, and no directory read to find out there was one.
func TestASearchWithNoScopeStaysInsideThisProject(t *testing.T) {
	mine := machine(t)
	agent := &Agent{config: Config{Place: Place{Dir: mine, Workspace: "/work/aforge"}}}

	scope, known := taskScopeWord("")
	if !known || scope != taskScopeProject {
		t.Fatalf("an absent scope read as %q (known %v), want %q", scope, known, taskScopeProject)
	}
	answer := agent.taskSearchText("", 0, scope)
	if strings.Contains(answer, "other projects") || strings.Contains(answer, "wisp") {
		t.Fatalf("an ordinary search reached outside this project:\n%s", answer)
	}
}

// The wide reading names the other project, its path and what it is running —
// and says nothing at all about the project that is quiet.
func TestScopeEverywhereListsTheOtherProjectAndSkipsTheQuietOne(t *testing.T) {
	mine := machine(t)
	agent := &Agent{config: Config{Place: Place{Dir: mine, Workspace: "/work/aforge"}}}

	answer := agent.taskSearchText("", 0, taskScopeEverywhere)
	for _, want := range []string{
		"running in other projects on this machine:",
		"wisp · /Users/ada/code/wisp",
		"another window · Port the parser · running · running for 2m",
		`in the window called "parser work"`,
		"files so far: internal/parse/lex.go",
	} {
		if !strings.Contains(answer, want) {
			t.Fatalf("the wide answer is missing %q:\n%s", want, answer)
		}
	}
	// THE EMPTINESS LAW. A project with nothing running is not a heading with
	// nothing under it — it is absent.
	if strings.Contains(answer, "quiet") {
		t.Fatalf("a project with nothing running was drawn:\n%s", answer)
	}
	// It is a reading of the OTHER projects: this one is already answered above
	// it, and a second listing of it would be the same window twice.
	if strings.Contains(answer, "/work/aforge") {
		t.Fatalf("this session's own project was listed as another one:\n%s", answer)
	}
}

// A ROW FROM ANOTHER PROJECT IS NOT ADDRESSABLE FROM HERE, and the answer says
// so once rather than leaving the model to discover it by calling with an id
// that means something else entirely in this project.
func TestRowsFromAnotherProjectCarryNoIDAndSayWhy(t *testing.T) {
	mine := machine(t)
	agent := &Agent{config: Config{Place: Place{Dir: mine, Workspace: "/work/aforge"}}}

	answer := agent.taskSearchText("", 0, taskScopeEverywhere)
	const limit = "These have no id in this conversation: work running in another project cannot be read, steered or resolved from here, and it lands where it is running rather than in this conversation."
	if !strings.Contains(answer, limit) {
		t.Fatalf("the wide answer never states the limit:\n%s", answer)
	}
	for _, row := range strings.Split(answer, "\n") {
		if !strings.Contains(row, "Port the parser") {
			continue
		}
		if strings.Contains(row, "· 4 ·") || strings.HasPrefix(strings.TrimSpace(row), "4 ·") {
			t.Fatalf("another project's row printed an id: %q", row)
		}
	}
}

// A CLAIM NOBODY HAS REFRESHED IS NOT A CLAIM ABOUT NOW. The rule is world.go's
// and this only proves it is the one being applied: a window whose presence
// file is older than the window aforge believes contributes nothing, exactly as
// a window that has closed does.
func TestAStalePresenceIsNotRunningAnywhere(t *testing.T) {
	root := t.TempDir()
	mine := machineSession(t, root, "here", "mine", "this chat", "/work/aforge", os.Getpid(), time.Second)
	machineSession(t, root, "wisp", "theirs", "parser work", "/Users/ada/code/wisp", 4242, presenceWindow+time.Minute,
		PresenceTask{ID: "4", Title: "Port the parser", State: string(TaskRunning), StartedAt: time.Now()})

	if groups := ReadOtherProjects(root, filepath.Join(root, "here"), time.Now(), os.Getpid()); len(groups) != 0 {
		t.Fatalf("a stale window was believed: %+v", groups)
	}
	agent := &Agent{config: Config{Place: Place{Dir: mine, Workspace: "/work/aforge"}}}
	if answer := agent.taskSearchText("", 0, taskScopeEverywhere); strings.Contains(answer, "Port the parser") {
		t.Fatalf("a stale window reached the answer:\n%s", answer)
	}
}

// A CONVERSATION THIS PROCESS IS HOLDING IS NOT SOMEWHERE TO GO. The keeper
// holds several projects in one terminal, and every one of them writes the same
// presence file every other window reads — so a row telling somebody to find a
// window they are two keystrokes from would be the refusal ReadElsewhere exists
// to stop this build making.
func TestAConversationThisTerminalHoldsReadsOpenHere(t *testing.T) {
	root := t.TempDir()
	mine := machineSession(t, root, "here", "mine", "this chat", "/work/aforge", os.Getpid(), time.Second)
	machineSession(t, root, "wisp", "kept", "docs", "/Users/ada/code/wisp", os.Getpid(), time.Second,
		PresenceTask{ID: "2", Title: "Rewrite the docs", State: string(TaskQueued)})

	agent := &Agent{config: Config{Place: Place{Dir: mine, Workspace: "/work/aforge"}}}
	answer := agent.taskSearchText("", 0, taskScopeEverywhere)
	if !strings.Contains(answer, "open here · Rewrite the docs · queued") {
		t.Fatalf("a conversation this terminal holds was not marked open here:\n%s", answer)
	}
	if strings.Contains(answer, "another window · Rewrite the docs") {
		t.Fatalf("a conversation this terminal holds was sent somewhere else:\n%s", answer)
	}
	if !strings.Contains(answer, "A row marked `open here` is a conversation this same terminal is already holding") {
		t.Fatalf("the open here label was drawn and never explained:\n%s", answer)
	}
	// A QUEUED NODE HAS NO CLOCK, so no age is invented for one.
	if strings.Contains(answer, "queued · running for") {
		t.Fatalf("a queued node was given an age:\n%s", answer)
	}
}

// A word that is neither is refused by name rather than quietly read as one of
// them: a model that guessed "all" should be told the two words, not handed
// this project's rows as though it had asked for them.
func TestAnUnknownScopeIsRefusedByName(t *testing.T) {
	agent := &Agent{config: Config{Place: Place{Dir: t.TempDir()}}}
	tool := agent.tasksTool()
	answer, isErr, err := tool.Execute(t.Context(), json.RawMessage(`{"scope":"all"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !isErr || !strings.Contains(answer, `"project"`) || !strings.Contains(answer, `"everywhere"`) {
		t.Fatalf("an unknown scope answered %q (error %v), want both words named", answer, isErr)
	}
}
