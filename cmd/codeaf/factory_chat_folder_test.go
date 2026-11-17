package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// chatFolderHome is a codeaf home with the factory floor where the desk keeps
// it (<home>/v3/factory), and the engine started INSIDE that home, as the
// WorkAF desk on one Mac started it.
func chatFolderHome(t *testing.T) (state string, floor *store.Store) {
	t.Helper()
	appDayProfile(t)
	state = home.Dir()
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	// What the sweep found there: codeaf's own state, never a project.
	if err := os.WriteFile(filepath.Join(state, "credentials.json"), []byte(`{"token":"sk-not-a-project"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	floor, err := store.Open(filepath.Join(state, "v3", "factory"))
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(state)
	return state, floor
}

// A STEP AGENTFIELD RAN NEVER WORKS IN THE ENGINE'S FOLDER, even when that
// folder is the codeaf home: its conversation, and the header the app opens it
// by, name the job's own empty folder under the floor.
func TestAnAgentFieldStepsChatNeverWorksInTheEnginesFolder(t *testing.T) {
	state, floor := chatFolderHome(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	it, err := floor.Create(factory.Item{Title: "swe-af pipeline step", RunOn: "swe-af.build", State: factory.StateRunning})
	if err != nil {
		t.Fatal(err)
	}
	r := &afRemote{items: floor, floor: floor}
	m := r.mirror(it, "exec-step", "build", true)
	if m == nil {
		t.Fatal("no conversation was made for the step")
	}
	meta, err := session.LoadMetaIn(session.DirOf(m.file), "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(floor.Root(), "chats", strconv.Itoa(it.ID))
	got := meta.Workspace
	if got != want {
		t.Fatalf("the step's chat works in %q; want the job's own folder %q", got, want)
	}
	if pathWithin(got, cwd) || pathWithin(got, state) {
		t.Fatalf("the step's chat works in %q, which holds the engine's folder %q or the codeaf home %q", got, cwd, state)
	}
	entries, err := os.ReadDir(got)
	if err != nil || len(entries) != 0 {
		t.Fatalf("the job's folder holds %d entries (%v); want it empty", len(entries), err)
	}
	header := afRecordLines(t, m.file)[0]
	if want := `"cwd":"` + want + `"`; !bytes.Contains(header, []byte(want)) {
		t.Fatalf("the chat's header is %s; want it to name %s", header, want)
	}
}

// A JOB AGENTFIELD RUNS HAS A MANAGER IN ITS OWN EMPTY FOLDER, not the
// checkout this machine happens to know: nothing of it runs here.
func TestARunOnJobsManagerWorksInItsOwnFolder(t *testing.T) {
	_, floor := chatFolderHome(t)
	checkout := t.TempDir()
	dirs := func(string) string { return checkout }
	it, err := floor.Create(factory.Item{Repo: "acme/ledger", Title: "refund", RunOn: "support.handle_ticket"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := talkFolder(floor, dirs, it)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(floor.Root(), "chats", strconv.Itoa(it.ID)); got != want {
		t.Fatalf("the RunOn job's manager works in %q; want %q", got, want)
	}
}

// A LOCAL JOB IS UNCHANGED: its manager works in its checkout, and its stage
// in its worktree under the floor, which is inside the codeaf home and still
// a project.
func TestALocalJobsChatsKeepTheirCheckout(t *testing.T) {
	_, floor := chatFolderHome(t)
	checkout := t.TempDir()
	it, err := floor.Create(factory.Item{Repo: "acme/ledger", Title: "double count"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := talkFolder(floor, func(string) string { return checkout }, it)
	if err != nil || got != checkout {
		t.Fatalf("the local job's manager works in %q (%v); want its checkout %q", got, err, checkout)
	}
	worktree := filepath.Join(floor.Root(), "work", "ledger-"+strconv.Itoa(it.ID))
	if err := factoryFolderRefusal(floor, worktree); err != nil {
		t.Fatalf("the job's worktree under the floor was refused: %v", err)
	}
	clone := factoryCloneFolder(floor.Root(), "acme/ledger")
	if err := factoryFolderRefusal(floor, clone); err != nil {
		t.Fatalf("the factory's clone was refused: %v", err)
	}
}

// THE GUARD REFUSES CODEAF'S OWN FOLDER from every road: the home, a folder
// above it, and a folder inside it that is not one job's own.
func TestTheFactoryRefusesAChatInCodeafsOwnFolder(t *testing.T) {
	state, floor := chatFolderHome(t)
	for _, dir := range []string{
		state,
		filepath.Dir(state),
		filepath.Join(state, "v3"),
		floor.Root(),
		filepath.Join(floor.Root(), "repos"),
		filepath.Join(floor.Root(), "work"),
		filepath.Join(state, "bin"),
	} {
		if err := factoryFolderRefusal(floor, dir); !errors.Is(err, errFactoryFolderIsHome) {
			t.Errorf("a factory chat in %q was allowed (%v); want it refused", dir, err)
		}
	}
	if err := factoryFolderRefusal(floor, t.TempDir()); err != nil {
		t.Errorf("a project folder outside the home was refused: %v", err)
	}
	// A host started in the home has no fallback folder at all.
	if got := factoryHostWorkspace(state); got != "" {
		t.Errorf("a host started in the codeaf home falls back to %q; want none", got)
	}
	if got := factoryHostWorkspace(filepath.Join(state, "v3")); got != "" {
		t.Errorf("a host started inside the codeaf home falls back to %q; want none", got)
	}
	if project := t.TempDir(); factoryHostWorkspace(project) != project {
		t.Error("a host started in a project lost that project as its fallback")
	}
}

// A FOREMAN AND A STAGE WITH NO FOLDER OF THEIR OWN fall back to an empty
// folder under the floor, never to the engine's.
func TestAFactoryWithNoWorkspaceGivesItsChatsAnEmptyFolder(t *testing.T) {
	state, floor := chatFolderHome(t)
	transcript, err := foremanMaker(floor, factoryHostWorkspace(state), t.TempDir(), nil)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	meta, err := session.LoadMetaIn(session.DirOf(transcript), "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(floor.Root(), "chats", "foreman"); meta.Workspace != want {
		t.Fatalf("the foreman works in %q; want %q", meta.Workspace, want)
	}
}

// THE APP'S OWN FACTORY HOST, started inside the codeaf home, keeps no
// fallback folder.
func TestAnAppFactoryHostStartedInTheHomeHasNoWorkspace(t *testing.T) {
	state, _ := chatFolderHome(t)
	h, err := newAppFactoryHost(context.Background(), state, "", "", newFactorySessions())
	if err != nil {
		t.Fatal(err)
	}
	defer h.close()
	if h.workspace != "" {
		t.Fatalf("the host started in the codeaf home falls back to %q; want none", h.workspace)
	}
}
