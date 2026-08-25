package store

import (
	"path/filepath"
	"testing"
	"time"
)

// The whole shape in one reading: three shelves, counted by kind and by status,
// with the machine's own totals beside them — and every status visible, which is
// the thing no other reader in this package can do.
func TestAMemorySnapshotShelvesEverythingAndCountsIt(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "snapshot.db"))
	mustAddMemory(t, graph, Memory{Type: MemoryPreference, Scope: MemoryScopeUser, Title: "Tabs", Text: "They indent with tabs."})
	mustAddMemory(t, graph, Memory{Type: MemoryFact, Scope: MemoryScopeUser, Title: "Timezone", Text: "They work from Bengaluru."})
	mustAddMemory(t, graph, Memory{Type: MemoryProjectState, Scope: MemoryScopeProject, Title: "Branch", Text: "Work lands on chat-v3-task."})
	mustAddMemory(t, graph, Memory{Type: MemoryFact, Scope: MemoryScopeEnv, Title: "Go", Text: "Go is on the path here."})
	letGo := mustAddMemory(t, graph, Memory{Type: MemoryFact, Scope: MemoryScopeUser, Title: "Wrong", Text: "This turned out to be wrong."})
	replaced := mustAddMemory(t, graph, Memory{Type: MemoryDecision, Scope: MemoryScopeProject, Title: "Old", Text: "The old decision."})
	if err := graph.ForgetMemory(letGo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.SupersedeMemory(replaced.ID, Memory{Type: MemoryDecision, Scope: MemoryScopeProject, Title: "New", Text: "The decision that stands."}); err != nil {
		t.Fatal(err)
	}

	snapshot, err := graph.MemorySnapshot(0)
	if err != nil {
		t.Fatalf("MemorySnapshot: %v", err)
	}
	if snapshot.Held != 5 || snapshot.LetGo != 1 || snapshot.Superseded != 1 {
		t.Fatalf("the machine totals are held %d, let go %d, superseded %d", snapshot.Held, snapshot.LetGo, snapshot.Superseded)
	}
	if snapshot.Total != 7 {
		t.Fatalf("the snapshot counts %d memories in all, want 7", snapshot.Total)
	}
	if len(snapshot.Shelves) != 3 {
		t.Fatalf("there are %d shelves, want the three scopes: %+v", len(snapshot.Shelves), snapshot.Shelves)
	}
	// THE ORDER IS THE ENUM'S, so two draws put the shelves in the same places.
	for i, want := range []string{MemoryScopeUser, MemoryScopeProject, MemoryScopeEnv} {
		if snapshot.Shelves[i].Scope != want {
			t.Fatalf("shelf %d is %q, want %q", i, snapshot.Shelves[i].Scope, want)
		}
	}
	user := snapshot.Shelves[0]
	if user.Held != 2 || user.LetGo != 1 || user.Superseded != 0 {
		t.Fatalf("the user shelf is %+v", user)
	}
	if user.ByType[MemoryFact] != 2 || user.ByType[MemoryPreference] != 1 {
		t.Fatalf("the user shelf's kinds are %+v", user.ByType)
	}
	if user.Label != "about you" {
		t.Fatalf("the user shelf is headed %q", user.Label)
	}
	project := snapshot.Shelves[1]
	if project.Held != 2 || project.Superseded != 1 {
		t.Fatalf("the project shelf is %+v", project)
	}
}

// A shelf's rows are the newest first, because that is the order a page reads a
// shelf in and the only one that does not need a second sort.
func TestAShelfsRowsAreNewestFirst(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "order.db"))
	first := mustAddMemory(t, graph, Memory{Type: MemoryFact, Scope: MemoryScopeUser, Title: "First", Text: "The first thing learned."})
	second := mustAddMemory(t, graph, Memory{Type: MemoryFact, Scope: MemoryScopeUser, Title: "Second", Text: "The second thing learned."})
	third := mustAddMemory(t, graph, Memory{Type: MemoryFact, Scope: MemoryScopeUser, Title: "Third", Text: "The third thing learned."})

	snapshot, err := graph.MemorySnapshot(0)
	if err != nil {
		t.Fatalf("MemorySnapshot: %v", err)
	}
	got := memoryIDs(snapshot.Shelves[0].Memories)
	want := []string{third.ID, second.ID, first.ID}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the shelf reads %v, want newest first %v", got, want)
		}
	}
}

// The limit caps the ROWS and never the counts: a page drawing ten of a thousand
// must still be able to say a thousand.
func TestTheLimitCapsTheRowsAndNeverTheCounts(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "limit.db"))
	for i := 0; i < 6; i++ {
		mustAddMemory(t, graph, Memory{Type: MemoryFact, Scope: MemoryScopeUser,
			Title: "Line", Text: "A thing worth remembering, number " + string(rune('a'+i)) + "."})
	}
	snapshot, err := graph.MemorySnapshot(2)
	if err != nil {
		t.Fatalf("MemorySnapshot: %v", err)
	}
	if snapshot.Held != 6 || snapshot.Total != 6 {
		t.Fatalf("the counts moved with the limit: held %d, total %d", snapshot.Held, snapshot.Total)
	}
	if snapshot.Shown != 2 || len(snapshot.Shelves[0].Memories) != 2 {
		t.Fatalf("the snapshot carries %d rows, want 2", snapshot.Shown)
	}
}

// An empty store is a person who has remembered nothing, which is a snapshot of
// nothing and never an error.
func TestAnEmptyStoreSnapshotsToNothing(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "empty.db"))
	snapshot, err := graph.MemorySnapshot(0)
	if err != nil {
		t.Fatalf("MemorySnapshot: %v", err)
	}
	if len(snapshot.Shelves) != 0 || snapshot.Total != 0 || snapshot.Held != 0 {
		t.Fatalf("an empty store snapshots to %+v", snapshot)
	}
	learned, letGo, err := graph.MemoryChangedSince(time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("MemoryChangedSince: %v", err)
	}
	if learned != 0 || letGo != 0 {
		t.Fatalf("an empty store changed by %d learned and %d let go", learned, letGo)
	}
}

// The "since you left" line's two figures, off the events that wrote each
// memory — the memories table has no timestamp of its own.
func TestMemoryChangedSinceCountsWhatWasLearnedAndLetGo(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "changed.db"))
	before := time.Now()
	old := mustAddMemory(t, graph, Memory{Type: MemoryFact, Scope: MemoryScopeUser, Title: "Old", Text: "Learned before they left."})

	// Everything after this instant is news.
	floor := time.Now().Add(time.Millisecond)
	time.Sleep(2 * time.Millisecond)
	mustAddMemory(t, graph, Memory{Type: MemoryFact, Scope: MemoryScopeProject, Title: "New", Text: "Learned while they were away."})
	mustAddMemory(t, graph, Memory{Type: MemoryFact, Scope: MemoryScopeProject, Title: "Also", Text: "Also learned while they were away."})
	if err := graph.ForgetMemory(old.ID); err != nil {
		t.Fatal(err)
	}

	learned, letGo, err := graph.MemoryChangedSince(floor)
	if err != nil {
		t.Fatalf("MemoryChangedSince: %v", err)
	}
	if learned != 2 {
		t.Fatalf("learned %d since they left, want 2", learned)
	}
	if letGo != 1 {
		t.Fatalf("let go of %d since they left, want 1", letGo)
	}
	// Everything since before any of it happened is everything.
	learned, letGo, err = graph.MemoryChangedSince(before.Add(-time.Second))
	if err != nil {
		t.Fatalf("MemoryChangedSince: %v", err)
	}
	if learned != 3 || letGo != 1 {
		t.Fatalf("since the beginning: %d learned, %d let go, want 3 and 1", learned, letGo)
	}
}

// WITH NO ORIGIN THERE IS NO SINCE. A first look that declared every memory ever
// learned to be news would be a delta with no delta in it.
func TestMemoryChangedSinceAnsweredNothingWithNoOrigin(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "origin.db"))
	mustAddMemory(t, graph, Memory{Type: MemoryFact, Scope: MemoryScopeUser, Title: "A", Text: "Something remembered."})
	learned, letGo, err := graph.MemoryChangedSince(time.Time{})
	if err != nil {
		t.Fatalf("MemoryChangedSince: %v", err)
	}
	if learned != 0 || letGo != 0 {
		t.Fatalf("a zero origin answered %d learned and %d let go, want nothing", learned, letGo)
	}
}

// A memory forgotten and then restored is not let go of any more: the line is
// about what stands now, not about what happened.
func TestARestoredMemoryIsNoLongerCountedAsLetGo(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "restored.db"))
	floor := time.Now().Add(-time.Hour)
	memory := mustAddMemory(t, graph, Memory{Type: MemoryFact, Scope: MemoryScopeUser, Title: "Back", Text: "This one came back."})
	if err := graph.ForgetMemory(memory.ID); err != nil {
		t.Fatal(err)
	}
	if _, letGo, err := graph.MemoryChangedSince(floor); err != nil || letGo != 1 {
		t.Fatalf("after forgetting: %d let go, %v", letGo, err)
	}
	if err := graph.RestoreMemory(memory.ID); err != nil {
		t.Fatal(err)
	}
	if _, letGo, err := graph.MemoryChangedSince(floor); err != nil || letGo != 0 {
		t.Fatalf("after restoring: %d let go, %v", letGo, err)
	}
}

// The kinds a section line names come out in a settled order, so one shelf does
// not read two ways.
func TestAShelfsKindsComeOutInASettledOrder(t *testing.T) {
	shelf := MemoryShelf{ByType: map[string]int{
		MemoryProjectState: 1, MemoryFact: 4, "invented": 2, MemoryPreference: 3, "empty": 0,
	}}
	want := []string{MemoryFact, MemoryPreference, MemoryProjectState, "invented"}
	got := MemoryShelfTypes(shelf)
	if len(got) != len(want) {
		t.Fatalf("the kinds are %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the kinds are %v, want %v", got, want)
		}
	}
}

// No machinery word reaches a shelf heading, and a scope this build does not
// know reads as nothing rather than as itself.
func TestTheShelfHeadingsAreWordsAPersonUses(t *testing.T) {
	for scope, want := range map[string]string{
		MemoryScopeUser:    "about you",
		MemoryScopeProject: "about a project",
		MemoryScopeEnv:     "about this machine",
		"":                 "",
		"repo:/tmp":        "",
	} {
		if got := MemoryShelfWord(scope); got != want {
			t.Fatalf("MemoryShelfWord(%q) = %q, want %q", scope, got, want)
		}
	}
}
