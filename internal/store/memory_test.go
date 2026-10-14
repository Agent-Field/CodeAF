package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The memory store's contract, one law per test: one write door that refuses
// duplicates within an owner and nowhere else, one reader whose order is the
// order a conversation carries, a forget that can be put back, a replacement
// that cannot cross owners, a move that carries the project's name, and a
// journal that rebuilds all of it — including the events earlier builds wrote
// and this one no longer reads.

func memoryStore(t *testing.T) *Store {
	t.Helper()
	return openTestStore(t, filepath.Join(t.TempDir(), "memory.db"))
}

func mustWrite(t *testing.T, s *Store, req WriteRequest) Memory {
	t.Helper()
	result, err := s.Write(req)
	if err != nil {
		t.Fatalf("write %q: %v", req.Text, err)
	}
	if result.Outcome != WriteOutcomeAdded {
		t.Fatalf("write %q = %s, want added", req.Text, result.Outcome)
	}
	return result.Memory
}

func TestTheWriteDoorKeepsWhoAndWhereAndDerivesTheTitle(t *testing.T) {
	t.Parallel()
	s := memoryStore(t)
	long := strings.Repeat("word ", 30) + "end"
	got := mustWrite(t, s, WriteRequest{
		Owner: OwnerProject("k1"), Text: long, Origin: OriginNoticed, ProjectName: "codeaf", SourceSession: "s1",
	})
	if got.Origin != OriginNoticed || got.ProjectName != "codeaf" || got.Scope != MemoryScopeProject {
		t.Fatalf("written = %+v, want the origin, project name and derived scope kept", got)
	}
	if got.Title != MemoryTitleOf(long) || !strings.HasSuffix(got.Title, "…") || len([]rune(got.Title)) > MemoryTitleRunes {
		t.Fatalf("title = %q, want the text's first words cut inside %d runes", got.Title, MemoryTitleRunes)
	}
	// A PROJECT NAME IS A PROJECT'S: on any other owner it is not kept, and an
	// origin this build does not spell is unknown rather than stored.
	other := mustWrite(t, s, WriteRequest{Owner: OwnerUser, Text: "prefers tabs", Origin: "robot", ProjectName: "codeaf"})
	if other.ProjectName != "" || other.Origin != "" {
		t.Fatalf("user line = %+v, want no project name and an unknown origin", other)
	}
}

func TestTheWriteDoorSkipsTheSameWordsWithinAnOwnerOnly(t *testing.T) {
	t.Parallel()
	s := memoryStore(t)
	first := mustWrite(t, s, WriteRequest{Owner: OwnerUser, Text: "Prefers   tabs"})
	again, err := s.Write(WriteRequest{Owner: OwnerUser, Text: "prefers tabs"})
	if err != nil {
		t.Fatal(err)
	}
	if again.Outcome != WriteOutcomeSkipped || again.Memory.ID != first.ID {
		t.Fatalf("same words again = %+v, want a skip naming the kept line", again)
	}
	// TWO OWNERS MAY HOLD THE SAME WORDS: a project's truth is not the person's.
	mustWrite(t, s, WriteRequest{Owner: OwnerProject("k1"), Text: "prefers tabs"})
}

func TestTheWriteDoorRefusesWhatItCannotKeep(t *testing.T) {
	t.Parallel()
	s := memoryStore(t)
	for _, req := range []WriteRequest{
		{Owner: "", Text: "no owner"},
		{Owner: OwnerUser, Text: "   "},
		{Owner: OwnerUser, Text: strings.Repeat("x", MemoryTextRunes+1)},
	} {
		if _, err := s.Write(req); !errors.Is(err, ErrInvalid) {
			t.Fatalf("write %+v err = %v, want ErrInvalid", req, err)
		}
	}
	// AND A CREDENTIAL NEVER LANDS, whatever the caller forgot.
	kept := mustWrite(t, s, WriteRequest{Owner: OwnerUser, Text: "the token is xoxb-1234567890abcdef"})
	if strings.Contains(kept.Text, "xoxb-1234567890abcdef") {
		t.Fatalf("kept %q, want the token redacted", kept.Text)
	}
}

func TestMemoriesForIsTheOrderAConversationCarries(t *testing.T) {
	t.Parallel()
	s := memoryStore(t)
	project := mustWrite(t, s, WriteRequest{Owner: OwnerProject("k1"), Text: "project line"})
	machine := mustWrite(t, s, WriteRequest{Owner: OwnerMachine, Text: "machine line"})
	userOld := mustWrite(t, s, WriteRequest{Owner: OwnerUser, Text: "older person line"})
	userNew := mustWrite(t, s, WriteRequest{Owner: OwnerUser, Text: "newer person line"})
	mustWrite(t, s, WriteRequest{Owner: OwnerProject("other"), Text: "another project"})
	gone := mustWrite(t, s, WriteRequest{Owner: OwnerUser, Text: "forgotten line"})
	if err := s.ForgetMemory(gone.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.MemoriesFor([]string{OwnerUser, OwnerMachine, OwnerProject("k1")})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{userOld.ID, userNew.ID, machine.ID, project.ID}
	if !reflect.DeepEqual(memoryIDs(got), want) {
		t.Fatalf("memories for = %v, want person (oldest first), machine, project: %v", memoryIDs(got), want)
	}
	if _, err := s.MemoriesFor(nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("memories for no owner err = %v, want ErrInvalid: unscoped is a leak", err)
	}
}

func TestAForgottenOrReplacedLineCanBePutBack(t *testing.T) {
	t.Parallel()
	s := memoryStore(t)
	owners := []string{OwnerUser}
	line := mustWrite(t, s, WriteRequest{Owner: OwnerUser, Text: "deploys on Fridays"})
	if err := s.ForgetMemoryForOwners(owners, line.ID, "s1"); err != nil {
		t.Fatal(err)
	}
	if err := s.ForgetMemoryForOwners(owners, line.ID, "s1"); err != nil {
		t.Logf("a second forget of a forgotten line answers %v", err)
	}
	if got, _ := s.MemoriesFor(owners); len(got) != 0 {
		t.Fatalf("forgotten line still carried: %v", memoryIDs(got))
	}
	if err := s.RestoreMemoryForOwners(owners, line.ID); err != nil {
		t.Fatalf("put back: %v", err)
	}
	// A REPLACEMENT IS UNDONE BY FORGETTING THE NEW LINE AND PUTTING THE OLD
	// ONE BACK, which is what a conversation's undo does.
	fresh, _, err := s.SupersedeMemoryForOwners(owners, line.ID, Memory{Owner: OwnerUser, Text: "deploys on Tuesdays"})
	if err != nil {
		t.Fatal(err)
	}
	old, _, _ := s.MemoryRecord(line.ID)
	if old.Status != MemorySuperseded {
		t.Fatalf("replaced line status = %q, want superseded", old.Status)
	}
	if err := s.ForgetMemoryForOwners(owners, fresh.ID, "s1"); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreMemoryForOwners(owners, line.ID); err != nil {
		t.Fatalf("put the replaced line back: %v", err)
	}
	got, _ := s.MemoriesFor(owners)
	if !reflect.DeepEqual(memoryIDs(got), []string{line.ID}) {
		t.Fatalf("after the undo = %v, want the original line alone", memoryIDs(got))
	}
}

func TestAReplacementCannotCrossOwners(t *testing.T) {
	t.Parallel()
	s := memoryStore(t)
	foreign := mustWrite(t, s, WriteRequest{Owner: OwnerProject("other"), Text: "their line"})
	if _, _, err := s.SupersedeMemoryForOwners([]string{OwnerUser, OwnerProject("k1")}, foreign.ID, Memory{Owner: OwnerProject("other"), Text: "x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("superseding a line this conversation cannot see err = %v, want ErrInvalid", err)
	}
	mine := mustWrite(t, s, WriteRequest{Owner: OwnerUser, Text: "my line"})
	if _, _, err := s.SupersedeMemoryForOwners([]string{OwnerUser}, mine.ID, Memory{Owner: OwnerMachine, Text: "moved by a replacement"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a replacement under another owner err = %v, want ErrInvalid", err)
	}
}

func TestFixingTheWordingFixesTheTitleAndKeepsTheRest(t *testing.T) {
	t.Parallel()
	s := memoryStore(t)
	line, err := s.AddMemory(Memory{Owner: OwnerUser, Text: "ships on Fridays", Tags: []string{"release"}, Origin: OriginNoticed})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateMemory(line.ID, "ships on Tuesdays"); err != nil {
		t.Fatal(err)
	}
	got, _, _ := s.MemoryRecord(line.ID)
	if got.Text != "ships on Tuesdays" || got.Title != "ships on Tuesdays" {
		t.Fatalf("after the fix = %q / %q, want both the words and the title moved", got.Text, got.Title)
	}
	if !reflect.DeepEqual(got.Tags, []string{"release"}) || got.Origin != OriginNoticed {
		t.Fatalf("after the fix = %+v, want the tags and origin kept", got)
	}
}

func TestMovingALineCarriesItsProjectName(t *testing.T) {
	t.Parallel()
	s := memoryStore(t)
	line := mustWrite(t, s, WriteRequest{Owner: OwnerUser, Text: "run make pr-ready first"})
	if _, err := s.MoveMemory(line.ID, OwnerProject("k1"), "codeaf"); err != nil {
		t.Fatal(err)
	}
	got, _, _ := s.MemoryRecord(line.ID)
	if got.Owner != OwnerProject("k1") || got.Scope != MemoryScopeProject || got.ProjectName != "codeaf" {
		t.Fatalf("moved line = %+v, want the project's owner, scope and name", got)
	}
	if _, err := s.MoveMemory(line.ID, OwnerMachine, "codeaf"); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.MemoryRecord(line.ID)
	if got.Owner != OwnerMachine || got.ProjectName != "" {
		t.Fatalf("moved to the machine = %+v, want no project name", got)
	}
	if _, err := s.MoveMemory(line.ID, OwnerLegacyProject, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("moving into the quarantine err = %v, want ErrInvalid", err)
	}
}

func TestRebuildReproducesEveryMemoryAndIgnoresRetiredEvents(t *testing.T) {
	t.Parallel()
	s := memoryStore(t)
	a := mustWrite(t, s, WriteRequest{Owner: OwnerUser, Text: "a", Origin: OriginPerson})
	b := mustWrite(t, s, WriteRequest{Owner: OwnerUser, Text: "b", Origin: OriginModel})
	c := mustWrite(t, s, WriteRequest{Owner: OwnerProject("k1"), Text: "c", Origin: OriginNoticed, ProjectName: "codeaf"})
	if err := s.ForgetMemory(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MoveMemory(c.ID, OwnerUser, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SupersedeMemoryForOwners([]string{OwnerUser}, a.ID, Memory{Owner: OwnerUser, Text: "a2", Origin: OriginNoticed}); err != nil {
		t.Fatal(err)
	}
	// THE EVENTS EARLIER BUILDS WROTE AND THIS ONE DOES NOT READ: a ranking
	// snapshot, a rule flag and a contextual evidence row. Each must replay as
	// nothing rather than fail the rebuild.
	tx, err := s.beginWrite()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []EventKind{"memory_ranking", "memory_always", "contextual_evidence"} {
		if _, _, err := appendEvent(tx, "memory", kind, map[string]any{"id": a.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	before := snapshotOf(t, s)
	if err := s.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := snapshotOf(t, s); !reflect.DeepEqual(before, after) {
		t.Fatalf("rebuild changed memory:\nbefore %v\nafter  %v", before, after)
	}
}

// snapshotOf is every memory row as a comparable summary.
func snapshotOf(t *testing.T, s *Store) []string {
	t.Helper()
	rows, err := s.queryMemories(``, nil, `memories.id`, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(rows))
	for _, m := range rows {
		out = append(out, strings.Join([]string{m.ID, m.Owner, m.Scope, m.Text, m.Status, m.Origin, m.ProjectName}, "|"))
	}
	return out
}

func TestTheSnapshotShelvesByOwnerAndKeepsTheLetGo(t *testing.T) {
	t.Parallel()
	s := memoryStore(t)
	mustWrite(t, s, WriteRequest{Owner: OwnerProject("k2"), Text: "zeta project", ProjectName: "zeta"})
	mustWrite(t, s, WriteRequest{Owner: OwnerProject("k1"), Text: "alpha project", ProjectName: "alpha"})
	mustWrite(t, s, WriteRequest{Owner: OwnerMachine, Text: "machine"})
	user := mustWrite(t, s, WriteRequest{Owner: OwnerUser, Text: "person"})
	gone := mustWrite(t, s, WriteRequest{Owner: OwnerUser, Text: "gone"})
	if err := s.ForgetMemory(gone.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddMemory(Memory{Scope: MemoryScopeProject, Text: "unplaced"}); err != nil {
		t.Fatal(err)
	}
	shelves, err := s.MemorySnapshot(0)
	if err != nil {
		t.Fatal(err)
	}
	var owners []string
	for _, shelf := range shelves.Shelves {
		owners = append(owners, shelf.Owner)
	}
	want := []string{OwnerUser, OwnerMachine, OwnerProject("k1"), OwnerProject("k2"), OwnerLegacyProject}
	if !reflect.DeepEqual(owners, want) {
		t.Fatalf("shelves = %v, want person, machine, projects by name, quarantine last: %v", owners, want)
	}
	if shelves.Shelves[0].Held != 1 || shelves.Shelves[0].Memories[0].ID != user.ID {
		t.Fatalf("person shelf = %+v, want the one held line", shelves.Shelves[0])
	}
	if shelves.Shelves[2].ProjectName != "alpha" {
		t.Fatalf("project shelf name = %q, want alpha", shelves.Shelves[2].ProjectName)
	}
	if shelves.Held != 5 || shelves.LetGoCount != 1 || len(shelves.LetGo) != 1 || shelves.LetGo[0].ID != gone.ID {
		t.Fatalf("totals = held %d let go %d %v, want 5 and the one forgotten line", shelves.Held, shelves.LetGoCount, memoryIDs(shelves.LetGo))
	}
}

func TestChangedSinceCountsWhatWasLearnedAndLetGo(t *testing.T) {
	t.Parallel()
	s := memoryStore(t)
	if learned, letGo, err := s.MemoryChangedSince(time.Time{}); err != nil || learned != 0 || letGo != 0 {
		t.Fatalf("since the zero instant = (%d, %d, %v), want zeros", learned, letGo, err)
	}
	floor := time.Now().Add(-time.Minute)
	line := mustWrite(t, s, WriteRequest{Owner: OwnerUser, Text: "a"})
	mustWrite(t, s, WriteRequest{Owner: OwnerUser, Text: "b"})
	if err := s.ForgetMemory(line.ID); err != nil {
		t.Fatal(err)
	}
	learned, letGo, err := s.MemoryChangedSince(floor)
	if err != nil || learned != 2 || letGo != 1 {
		t.Fatalf("since a minute ago = (%d, %d, %v), want 2 learned and 1 let go", learned, letGo, err)
	}
}

func TestOnlyQuarantinedLinesAreRehomed(t *testing.T) {
	t.Parallel()
	s := memoryStore(t)
	lost, err := s.AddMemory(Memory{Scope: MemoryScopeProject, Text: "came in blind"})
	if err != nil {
		t.Fatal(err)
	}
	if lost.Owner != OwnerLegacyProject || !containsStubTag(lost.Tags, OwnerLegacyTag) {
		t.Fatalf("a bare project write = %+v, want the quarantine and its mark", lost)
	}
	mine := mustWrite(t, s, WriteRequest{Owner: OwnerUser, Text: "mine"})
	moved, err := s.RehomeMemories([]string{lost.ID, mine.ID}, OwnerProject("k1"))
	if err != nil || moved != 1 {
		t.Fatalf("rehome = (%d, %v), want only the quarantined line moved", moved, err)
	}
	got, _, _ := s.MemoryRecord(mine.ID)
	if got.Owner != OwnerUser {
		t.Fatalf("a placed line was rehomed: %+v", got)
	}
}
