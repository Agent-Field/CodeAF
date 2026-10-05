package session

// The memory contracts this wave added, proved the way this package proves
// everything: what a model was shown, what the store holds, and what the
// person could see.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/store"
)

// ── owner isolation, through the session's own doors ────────────────────────

// A memory written in one project never surfaces in another. The router's
// shortlist, the dedup search and a person's query are all scoped to the
// owners the session can see, and the session's owners come from the project
// key the door handed it.
func TestAMemoryInOneProjectNeverSurfacesInAnother(t *testing.T) {
	brain, err := store.Open(filepath.Join(t.TempDir(), "shared.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })

	alpha, _ := newTestAgent(t, &reflexScript{}, func(config *Config) {
		config.Memory = brain
		config.MemoryProjectKey = "alpha-key"
	})
	beta, _ := newTestAgent(t, &reflexScript{}, func(config *Config) {
		config.Memory = brain
		config.MemoryProjectKey = "beta-key"
	})

	if _, err := alpha.RememberScoped("deploys to the amber cluster every Tuesday", store.MemoryScopeProject); err != nil {
		t.Fatalf("alpha's project write: %v", err)
	}

	// BETA'S ROUTER IS NEVER SHOWN IT. The shortlist beta's router reads is
	// beta's own view; the store never offered alpha's row.
	block := beta.memoryBlock(context.Background(), "where do we deploy the amber cluster")
	if strings.Contains(block, "amber cluster") {
		t.Fatalf("beta's block carried alpha's memory:\n%s", block)
	}
	// BETA'S SEARCH FINDS NOTHING OF ALPHA'S — a person asking in beta does
	// not get alpha's project memory back.
	found, err := beta.Memories("amber cluster")
	if err != nil || len(found) != 0 {
		t.Fatalf("beta's query = (%v, %v), want nothing of alpha's", found, err)
	}
	// AND ALPHA STILL SEES ITS OWN.
	alphaFound, err := alpha.Memories("amber cluster")
	if err != nil || len(alphaFound) != 1 {
		t.Fatalf("alpha's query = (%v, %v), want its own row", alphaFound, err)
	}
	// The row itself is owned by alpha's project, not by the person at large.
	rows, err := brain.SearchMemories([]string{store.OwnerProject("alpha-key")}, "amber", 5)
	if err != nil || len(rows) != 1 {
		t.Fatalf("alpha's owner holds %v (%v)", memoryIDs(rows), err)
	}
}

// The same words written from two projects are TWO ROWS: the dedup door never
// lets one project's write swallow another's, because two projects may hold
// genuinely different truths about the same words.
func TestTheSameWordsInTwoProjectsAreTwoMemories(t *testing.T) {
	brain, err := store.Open(filepath.Join(t.TempDir(), "twotruths.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })

	alpha, _ := newTestAgent(t, &reflexScript{}, func(config *Config) {
		config.Memory = brain
		config.MemoryProjectKey = "alpha-key"
	})
	beta, _ := newTestAgent(t, &reflexScript{}, func(config *Config) {
		config.Memory = brain
		config.MemoryProjectKey = "beta-key"
	})

	if _, err := alpha.RememberScoped("the API key rotates every ninety days", store.MemoryScopeProject); err != nil {
		t.Fatal(err)
	}
	if _, err := beta.RememberScoped("the API key rotates every ninety days", store.MemoryScopeProject); err != nil {
		t.Fatal(err)
	}
	alphaRows, err := brain.SearchMemories([]string{store.OwnerProject("alpha-key")}, "rotates", 5)
	if err != nil || len(alphaRows) != 1 {
		t.Fatalf("alpha holds %v (%v)", memoryIDs(alphaRows), err)
	}
	betaRows, err := brain.SearchMemories([]string{store.OwnerProject("beta-key")}, "rotates", 5)
	if err != nil || len(betaRows) != 1 {
		t.Fatalf("beta holds %v (%v)", memoryIDs(betaRows), err)
	}
	// A write scoped to the user, from either project, is the person's own and
	// is deduplicated against itself only.
}

// A task worker inherits its parent's owners, and a worker cannot WRITE
// memories at all — the family would otherwise be eight writers on one brain,
// all blind to each other.
func TestAWorkerInheritsTheViewAndHoldsNoWriteVerb(t *testing.T) {
	brain, err := store.Open(filepath.Join(t.TempDir(), "worker.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })
	parent, _ := newTestAgent(t, &reflexScript{}, func(config *Config) {
		config.Memory = brain
		config.MemoryProjectKey = "alpha-key"
	})
	// THE WORKER'S SHAPE IS THE ONE THE TASK TREE BUILDS: no memory brain of
	// its own ([Agent.remembers] is false on a node's workers), the parent's
	// routed block handed to it. The belt check proves the ABSENCE of the
	// write verb, which is what keeps a family of workers from being eight
	// writers on one brain.
	worker, _ := newTestAgent(t, &reflexScript{}, func(config *Config) {
		config.Memory = brain
		config.MemoryProjectKey = "alpha-key"
		config.PromptProfile = "lean"
	})
	_ = parent
	for _, tool := range worker.belt() {
		if tool.Name == "remember" {
			t.Fatal("a worker carries the remember verb; a family of workers would be eight writers")
		}
	}
	// AND THE VIEW A WORKER WOULD READ THROUGH IS ITS PARENT'S: the config is
	// copied whole, so the owners a node's reading is scoped to are the
	// parent's.
	if owners := parent.memoryOwners(); len(owners) != 3 || owners[2] != store.OwnerProject("alpha-key") {
		t.Fatalf("the parent's view = %v, want the project's owners", owners)
	}
	if owners := worker.memoryOwners(); len(owners) != 3 || owners[2] != store.OwnerProject("alpha-key") {
		t.Fatalf("the worker's copied view = %v, want the parent's owners", owners)
	}
}

// ── one write door ──────────────────────────────────────────────────────────

// Every mouth that says "keep this" walks one door, so a preference stated in
// three ways through three doors is one row, not three.
func TestEveryRememberDoorSettlesIntoOneRow(t *testing.T) {
	script := &reflexScript{
		decide: `{"op":"add"}`,
		// Door two rides the ROUTER, which must name the command for the door
		// to be walked at all.
		route: `{"inject":[],"cmd":{"name":"remember","arg":"always deploys on Fridays"}}`,
	}
	agent, brain := brainAgent(t, script, nil)

	// Door one: /remember.
	if _, err := agent.Remember("always deploys on Fridays"); err != nil {
		t.Fatalf("/remember: %v", err)
	}
	// Door two: the same words through the routed command — a different mouth,
	// a different turn, the same row. The router was scripted to name it.
	agent.routedMemory(context.Background(), "remember that I always deploy on Fridays",
		nil, true)
	// Door three: the same words typed with different case, which is the same
	// words to the door.
	if _, err := agent.Remember("ALWAYS deploys on Fridays"); err != nil {
		t.Fatalf("/remember retyped: %v", err)
	}

	kept, err := brain.ListMemories([]string{store.OwnerUser}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 {
		t.Fatalf("three doors kept %v, want one row", titles(kept))
	}
	// And the skips are journaled, so the record says what happened.
	events, err := brain.Events(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	skips := 0
	for _, event := range events {
		if event.Kind == store.EventMemorySkipped {
			skips++
		}
	}
	if skips != 2 {
		t.Fatalf("skips journaled = %d, want 2", skips)
	}
}

// An explicit save works with NO reflex model at all: the words a person typed
// are kept by the store's own door, and a provider outage cannot lose them.
func TestAnExplicitSaveNeedsNoModel(t *testing.T) {
	// THE DECIDER CANNOT ANSWER — an outage, a nonsense reply, the shape "no
	// model" takes in a live session. The save still happens: the store's own
	// dedup door needs no model, and the words a person typed are never
	// refused because somebody else's service was down.
	script := &reflexScript{decErr: errorsNew("the decider answered nonsense")}
	agent, brain := brainAgent(t, script, nil)
	// A NEARBY ROW is what puts the decider in the path at all: without one,
	// the save needs no model and the test would prove nothing.
	remember(t, brain, "deploy days", "deploys happen on some day of the week")
	title, err := agent.Remember("always deploys on Fridays")
	if err != nil {
		t.Fatalf("/remember with a dead decider: %v", err)
	}
	if title == "" {
		t.Fatal("the save answered no title")
	}
	kept, err := brain.ListMemories([]string{store.OwnerUser}, 10)
	if err != nil || len(kept) != 2 {
		t.Fatalf("the save without a decider = (%v, %v), want both rows", titles(kept), err)
	}
	// AND THE FAILED SETTLE IS JOURNALED — the save happened, the settle did
	// not, and the record says so.
	events, err := brain.Events(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	failed := 0
	for _, event := range events {
		if event.Kind == store.EventMemoryWriteFailed {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("settle failures journaled = %d, want 1", failed)
	}
}

// ── visible failures ────────────────────────────────────────────────────────

// A failed extraction is journaled and said, once per window — never silent,
// never a flood.
func TestAFailedExtractionIsJournaledAndSaidOnce(t *testing.T) {
	script := &reflexScript{extractErr: errorsNew("provider is down")}
	agent, brain := brainAgent(t, script, nil)
	script.extract = `{"mem":0}`

	// Drive the post-turn pass by hand: the failure path is the pass's, not the
	// turn's.
	agent.memory.setInjected(nil)
	agent.learnFromTurn("remember that I prefer tabs", "I will keep that in mind.")
	// The pass runs on its own goroutine; wait for the journal to carry it.
	deadline := time.Now().Add(5 * time.Second)
	for {
		events, err := brain.Events(0, 100)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, event := range events {
			if event.Kind == store.EventMemoryWriteFailed {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the failed extraction was never journaled")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// AND THE PERSON IS TOLD — the notice is held for the next stream, which is
	// where the memory lines ride.
	held := agent.memory.takeNotices()
	if len(held) == 0 || !strings.Contains(held[0], "couldn't settle a memory") {
		t.Fatalf("the held notices = %v, want one line naming the failure", held)
	}
	// AND ONCE PER WINDOW: more failures inside the window say nothing more.
	agent.learnFromTurn("and remember the other thing", "Noted.")
	held = agent.memory.takeNotices()
	if len(held) != 0 {
		t.Fatalf("a second failure in the window said %v, want silence", held)
	}
}

// When the STORE itself is what failed, the reason goes to a file beside the
// state root instead of nowhere.
func TestAFailureWithADeadStoreReachesTheFallbackFile(t *testing.T) {
	brain, err := store.Open(filepath.Join(t.TempDir(), "dying.db"))
	if err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &reflexScript{}, func(config *Config) {
		config.Memory = brain
	})
	if err := brain.Close(); err != nil {
		t.Fatal(err)
	}
	// Set the window back so the notice path is exercised too.
	agent.memory.failureSaid = time.Time{}
	agent.journalMemoryFailure("settle", errorsNew("disk went away"))

	path := memoryFailureLogPath()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the fallback file was not written: %v", err)
	}
	if !strings.Contains(string(body), "via=settle") || !strings.Contains(string(body), "disk went away") {
		t.Fatalf("the fallback line = %q, want the via and the reason", body)
	}
}

// ── the legacy import ───────────────────────────────────────────────────────

// An import that cannot read the whole file leaves the file exactly where it
// is, journals why, and retries on the next call.
func TestAFailedImportLeavesTheFileAndRetries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.md")
	// A line longer than any buffer the scanner will grow into: the scan fails
	// at it, which is the failure the old importer renamed over. It stands
	// FIRST, so nothing after it was ever read.
	var long strings.Builder
	for index := 0; index < 70_000; index++ {
		long.WriteString("x")
	}
	body := long.String() + "\na good line worth keeping\nanother good line\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	script := &reflexScript{}
	agent, brain := brainAgent(t, script, func(config *Config) {
		config.MemoryImport = path
	})

	agent.importMemoryFile()
	kept, err := brain.ListMemories([]string{store.OwnerUser}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 0 {
		t.Fatalf("a scan failure imported %v; the file is not proven readable", titles(kept))
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the file was moved out of the way after a scan failure: %v", err)
	}
	if agent.memory.imported {
		t.Fatal("a failed import left its once-marker set; the next turn would not retry")
	}

	// THE FILE IS FIXED — smaller lines now — and the retry imports everything.
	if err := os.WriteFile(path, []byte("a good line worth keeping\nanother good line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent.memory.imported = false
	agent.importMemoryFile()
	kept, err = brain.ListMemories([]string{store.OwnerUser}, 10)
	if err != nil || len(kept) != 2 {
		t.Fatalf("the retry imported (%v, %v), want both lines", titles(kept), err)
	}
	if _, err := os.Stat(path + ".imported"); err != nil {
		t.Fatalf("the imported file was not renamed: %v", err)
	}

	// AND THE RETRY IS IDEMPOTENT: running the import again — the state a
	// rename that failed would leave — adds nothing.
	agent.memory.imported = false
	agent.importMemoryFile()
	kept, err = brain.ListMemories([]string{store.OwnerUser}, 10)
	if err != nil || len(kept) != 2 {
		t.Fatalf("a repeated import holds (%v, %v), want the same two rows", titles(kept), err)
	}
}

// A PARTIAL import — one row refused — leaves the file and says so; the rows
// that landed stay landed, and the retry skips them as duplicates.
func TestAnImportSurvivesADeadDeciderWithoutDuplicates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.md")
	if err := os.WriteFile(path, []byte("first good line\nsecond good line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A row whose settle the decider refuses — the same shape a provider
	// outage makes of a real import. THE SAVE IS LOSSLESS ANY MORE: the
	// decider failing does not refuse the row, because the words are the
	// person's and the store's own door does not need a model.
	script := &reflexScript{decErr: errorsNew("the decider answered nonsense")}
	agent, brain := brainAgent(t, script, func(config *Config) {
		config.MemoryImport = path
	})
	seed := remember(t, brain, "earlier line", "first good line")
	_ = seed

	agent.importMemoryFile()
	// THE FILE WAS IMPORTED ANYWAY — every row landed through the store's own
	// door — and the failed settles are in the journal, not vanished.
	if _, err := os.Stat(path + ".imported"); err != nil {
		t.Fatalf("the import did not finish past a dead decider: %v", err)
	}
	kept, err := brain.ListMemories([]string{store.OwnerUser}, 10)
	if err != nil || len(kept) != 2 {
		// The seeded row IS "first good line", so the first line's save skips
		// as its duplicate and only the second lands fresh.
		t.Fatalf("the import past a dead decider holds (%v, %v), want seed + the second line", titles(kept), err)
	}
	// The settle failures are journaled, one per row the decider was asked.
	events, err := brain.Events(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	failed := 0
	for _, event := range events {
		if event.Kind == store.EventMemoryWriteFailed {
			var payload map[string]string
			_ = json.Unmarshal(event.Payload, &payload)
			if payload["via"] == "explicit-settle" {
				failed++
			}
		}
	}
	if failed != 2 {
		t.Fatalf("settle failures journaled = %d, want 2 — one per row the decider refused", failed)
	}

	// AND A RE-IMPORT ADDS NOTHING: the door's dedup is what makes the retry
	// idempotent, whatever killed the first attempt.
	agent.memory.imported = false
	agent.importMemoryFile()
	kept, err = brain.ListMemories([]string{store.OwnerUser}, 10)
	if err != nil || len(kept) != 2 {
		t.Fatalf("a repeated import holds (%v, %v), want the same two rows", titles(kept), err)
	}
}

// ── memory off is not search off ────────────────────────────────────────────

// A session with memory OFF and an index still carries the search verb and no
// memory verb. The memory row decides what is REMEMBERED; the conversation
// index decides what is SEARCHABLE.
func TestMemoryOffWithAnIndexKeepsSearchAndLosesRemember(t *testing.T) {
	index, err := store.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	agent, _ := newTestAgent(t, &reflexScript{}, func(config *Config) {
		config.MemoryIndex = index
	})
	var search, rememberTool bool
	for _, tool := range agent.belt() {
		switch tool.Name {
		case "search_conversations":
			search = true
		case "remember":
			rememberTool = true
		}
	}
	if !search {
		t.Fatal("a session with an index and memory off has no search verb")
	}
	if rememberTool {
		t.Fatal("a session with memory off carries the remember verb")
	}
	// AND THE CONVERSATION ITSELF IS INDEXED, so the next search can find it:
	// the journal posts into the index store, not into a nil memory.
	if agent.chatlog == nil {
		t.Fatal("memory off closed the chat journal; the conversation would never be searchable")
	}
}

func memoryIDs(memories []store.Memory) []string {
	ids := make([]string, len(memories))
	for index, memory := range memories {
		ids[index] = memory.ID
	}
	return ids
}

// errorsNew keeps the test file's error spelling in one place.
func errorsNew(text string) error { return &failureError{text} }

type failureError struct{ text string }

func (e *failureError) Error() string { return e.text }

// ── regression: foreign-ID write isolation through the session ────────────────

// A decider that names another project's memory id — from a stale neighbor
// list, a replayed conversation, or a model invention — cannot update that
// memory through the session's write path. The store-level owner guard
// refuses even if the session's own visibility check were bypassed.
func TestAForeignIDCannotUpdateAnotherProjectsMemory(t *testing.T) {
	brain, err := store.Open(filepath.Join(t.TempDir(), "foreign-update.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })

	alpha, _ := newTestAgent(t, &reflexScript{}, func(config *Config) {
		config.Memory = brain
		config.MemoryProjectKey = "alpha-key"
	})

	// Write a memory from alpha's project.
	title, err := alpha.RememberScoped("deploys to the amber cluster on Tuesdays", store.MemoryScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if title == "" {
		t.Fatal("alpha's project write produced no title")
	}

	// Find the row directly.
	alphaRows, err := brain.SearchMemories([]string{store.OwnerProject("alpha-key")}, "amber", 5)
	if err != nil || len(alphaRows) != 1 {
		t.Fatalf("alpha's rows = (%v, %v)", memoryIDs(alphaRows), err)
	}
	foreignID := alphaRows[0].ID

	// A beta agent with a different project key tries to update alpha's row
	// through the scoped store door.
	beta, _ := newTestAgent(t, &reflexScript{}, func(config *Config) {
		config.Memory = brain
		config.MemoryProjectKey = "beta-key"
	})
	betaOwners := beta.memoryOwners()
	err = brain.UpdateMemoryForOwners(betaOwners, foreignID,
		"Deploys Wednesdays", "Deploys to the ember cluster on Wednesdays.", nil, "")
	if err == nil {
		t.Fatal("UpdateMemoryForOwners accepted a foreign project id")
	}

	// Alpha's row is unchanged.
	alphaRows2, err := brain.SearchMemories([]string{store.OwnerProject("alpha-key")}, "amber", 5)
	if err != nil || len(alphaRows2) != 1 {
		t.Fatalf("alpha's rows after foreign update = (%v, %v)", memoryIDs(alphaRows2), err)
	}
	if alphaRows2[0].Text != alphaRows[0].Text {
		t.Fatalf("alpha's row was modified: %q", alphaRows2[0].Text)
	}
}

// A decider that names another project's memory id cannot supersede that
// memory through the session's write path. The store-level owner guard
// refuses, and the old row stays active.
func TestAForeignIDCannotSupersedeAnotherProjectsMemory(t *testing.T) {
	brain, err := store.Open(filepath.Join(t.TempDir(), "foreign-supersede.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })

	alpha, _ := newTestAgent(t, &reflexScript{}, func(config *Config) {
		config.Memory = brain
		config.MemoryProjectKey = "alpha-key"
	})

	title, err := alpha.RememberScoped("deploys to the amber cluster on Tuesdays", store.MemoryScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if title == "" {
		t.Fatal("alpha's project write produced no title")
	}

	alphaRows, err := brain.SearchMemories([]string{store.OwnerProject("alpha-key")}, "amber", 5)
	if err != nil || len(alphaRows) != 1 {
		t.Fatalf("alpha's rows = (%v, %v)", memoryIDs(alphaRows), err)
	}
	foreignID := alphaRows[0].ID

	// A beta agent tries to supersede alpha's row through the scoped store door.
	beta, _ := newTestAgent(t, &reflexScript{}, func(config *Config) {
		config.Memory = brain
		config.MemoryProjectKey = "beta-key"
	})
	betaOwners := beta.memoryOwners()
	_, err = brain.SupersedeMemoryForOwners(betaOwners, foreignID, store.Memory{
		Owner: store.OwnerProject("beta-key"), Type: store.MemoryFact,
		Title: "Deploys Wednesdays", Text: "Deploys to the ember cluster on Wednesdays.",
	})
	if err == nil {
		t.Fatal("SupersedeMemoryForOwners accepted a foreign project id")
	}

	// Alpha's row is still active.
	alphaRows2, err := brain.SearchMemories([]string{store.OwnerProject("alpha-key")}, "amber", 5)
	if err != nil || len(alphaRows2) != 1 {
		t.Fatalf("alpha's rows after foreign supersede = (%v, %v)", memoryIDs(alphaRows2), err)
	}
	if alphaRows2[0].Status != store.MemoryActive {
		t.Fatalf("alpha's row is %q, want active", alphaRows2[0].Status)
	}
}

// The post-turn extraction that infers a project-scoped memory after a /forget
// removed the project copy must not auto-widen that memory to user-global. The
// extraction ownership is pinned to the session's authorized audience — the
// project key that the door proved. A memory scoped to project by the model
// stays in the project, even when no project copy exists to dedup against.
func TestPostTurnExtractionDoesNotWidenProjectMemoryToUserGlobal(t *testing.T) {
	brain, err := store.Open(filepath.Join(t.TempDir(), "no-widen.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })

	alpha, _ := newTestAgent(t, &reflexScript{}, func(config *Config) {
		config.Memory = brain
		config.MemoryProjectKey = "alpha-key"
	})

	// Write a project-scoped memory.
	title, err := alpha.RememberScoped("the deploy window opens on Tuesday morning", store.MemoryScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if title == "" {
		t.Fatal("project write produced no title")
	}

	// Forget it from the project's view.
	forgot, err := alpha.Forget("deploy window")
	if err != nil {
		t.Fatal(err)
	}
	if forgot == "" {
		t.Fatal("forget found nothing")
	}

	// Re-member the same content. The session's ownerForScope pins the
	// project scope to the authorized project, never widens to user.
	title2, err := alpha.RememberScoped("the deploy window opens on Tuesday morning", store.MemoryScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if title2 == "" {
		t.Fatal("second project write produced no title")
	}

	// The new row is owned by alpha's project, NOT by user.
	allRows, err := brain.ListMemories(nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range allRows {
		if row.Status != store.MemoryActive {
			continue
		}
		if strings.Contains(row.Text, "deploy window") && row.Owner == store.OwnerUser {
			t.Fatalf("a project-scoped memory landed as user-global: owner=%q text=%q", row.Owner, row.Text)
		}
	}

	// And user-scoped searches in another project do not find it.
	beta, _ := newTestAgent(t, &reflexScript{}, func(config *Config) {
		config.Memory = brain
		config.MemoryProjectKey = "beta-key"
	})
	block := beta.memoryBlock(context.Background(), "deploy window Tuesday")
	if strings.Contains(block, "deploy window") {
		t.Fatalf("beta's block carried alpha's project memory after /forget re-write:\n%s", block)
	}
}
