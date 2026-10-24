package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Prompt capacity limits admission, never storage. Every mutation remains
// transactional while permitting a working set smaller than the database.

// roomLine is a line of about ninety runes, numbered so no two are the same
// words.
func roomLine(prefix string, n int) string {
	return fmt.Sprintf("%s line %03d %s", prefix, n, strings.Repeat("w", 70))
}

// fillTo adds lines to owner with the raw door (unbounded) until the owner's
// lines cost at least want runes, and answers what they cost.
func fillTo(t *testing.T, s *Store, owner, name, prefix string, want int) int {
	t.Helper()
	cost := 0
	for n := 0; cost < want; n++ {
		text := roomLine(prefix, n)
		if _, err := s.AddMemory(Memory{Owner: owner, Type: MemoryFact, Text: text, ProjectName: name}); err != nil {
			t.Fatal(err)
		}
		cost += MemoryLineCost(text)
	}
	return cost
}

func TestAnEverywhereLineIsIndependentOfOtherProjectsCapacity(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	fillTo(t, s, OwnerProject("beta"), "beta-repo", "beta", 2*MemoryBlockRunes)
	for _, owner := range []string{OwnerUser, OwnerMachine, OwnerProject("alpha")} {
		if _, err := s.Write(WriteRequest{Owner: owner, Type: MemoryPreference, Text: roomLine("new", 1)}); err != nil {
			t.Fatalf("write for %s: %v", owner, err)
		}
	}
}

func TestDuplicateOwnershipIsIndependentOfPromptCapacity(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	text := roomLine("shared", 1)
	if _, err := s.AddMemory(Memory{Owner: OwnerProject("alpha"), Type: MemoryFact, Text: text}); err != nil {
		t.Fatal(err)
	}
	fillTo(t, s, OwnerUser, "", "person", 2*MemoryBlockRunes)
	if got, err := s.Write(WriteRequest{Owner: OwnerUser, Type: MemoryFact, Text: text}); err != nil || got.Outcome != WriteOutcomeAdded {
		t.Fatalf("other owner: %+v %v", got, err)
	}
	if got, err := s.Write(WriteRequest{Owner: OwnerProject("alpha"), Type: MemoryFact, Text: text}); err != nil || got.Outcome != WriteOutcomeSkipped {
		t.Fatalf("same owner: %+v %v", got, err)
	}
}

func TestConcurrentWritersBothSucceedBeyondPromptCapacity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	first := openTestStore(t, path)
	second := openTestStore(t, path)
	fillTo(t, first, OwnerUser, "", "person", 2*MemoryBlockRunes)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, s := range []*Store{first, second} {
		wg.Add(1)
		go func(i int, s *Store) {
			defer wg.Done()
			_, errs[i] = s.Write(WriteRequest{Owner: OwnerUser, Type: MemoryFact, Text: roomLine("writer", i)})
		}(i, s)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("concurrent writer: %v", err)
		}
	}
	block, err := first.AssembleMemoryBlock(MemoryContext{Owners: []string{OwnerUser}})
	if err != nil {
		t.Fatal(err)
	}
	cost := 0
	for _, m := range block.Memories {
		cost += MemoryLineCost(m.Text)
	}
	if cost > MemoryBlockRunes || block.Omitted == 0 {
		t.Fatalf("admitted cost=%d omitted=%d", cost, block.Omitted)
	}
}

func TestAReplacementCanGrowBeyondPromptCapacity(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	old, err := s.AddMemory(Memory{Owner: OwnerUser, Type: MemoryFact, Text: "short"})
	if err != nil {
		t.Fatal(err)
	}
	fillTo(t, s, OwnerUser, "", "person", 2*MemoryBlockRunes)
	replacement, added, err := s.SupersedeMemoryForOwners([]string{OwnerUser}, old.ID, Memory{Owner: OwnerUser, Type: MemoryFact, Text: strings.Repeat("longer ", 36)})
	if err != nil || !added {
		t.Fatalf("replacement: added=%v err=%v", added, err)
	}
	prior, found, err := s.MemoryRecord(old.ID)
	if err != nil || !found || prior.Status != MemorySuperseded {
		t.Fatalf("previous status=%s found=%v err=%v", prior.Status, found, err)
	}
	if replacement.Status != MemoryActive {
		t.Fatal("replacement not active")
	}
}

func TestAllWritesAreUnboundedRegardlessOfLegacyFlag(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	fillTo(t, s, OwnerUser, "", "person", 2*MemoryBlockRunes)
	for i, legacy := range []bool{true, false} {
		got, err := s.Write(WriteRequest{Owner: OwnerUser, Type: MemoryFact, Text: roomLine("imported", i), Unbounded: legacy})
		if err != nil || got.Outcome != WriteOutcomeAdded {
			t.Fatalf("legacy=%v result=%+v err=%v", legacy, got, err)
		}
	}
}

// WORDS ALREADY KEPT ARE NOT KEPT TWICE: putting back a line whose words its
// owner keeps again since, or rewording one onto another line's words, is
// refused with [ErrMemoryKept], and one line still says it.
func TestPuttingBackOrRewordingOntoWordsAlreadyKeptKeepsOneLine(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	gone, err := s.AddMemory(Memory{Owner: OwnerUser, Type: MemoryFact, Text: "prefers tabs"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ForgetMemory(gone.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write(WriteRequest{Owner: OwnerUser, Type: MemoryFact, Text: "Prefers  tabs"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreMemory(gone.ID); !errors.Is(err, ErrMemoryKept) {
		t.Fatalf("putting back words already kept = %v; want ErrMemoryKept", err)
	}
	other, err := s.AddMemory(Memory{Owner: OwnerUser, Type: MemoryFact, Text: "uses pnpm"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateMemory(other.ID, "prefers tabs"); !errors.Is(err, ErrMemoryKept) {
		t.Fatalf("rewording onto words already kept = %v; want ErrMemoryKept", err)
	}
	active, err := s.MemoriesFor([]string{OwnerUser})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 2 {
		t.Fatalf("active lines = %+v, want the one kept and the one left as it was", active)
	}
	// THE SAME WORDS FOR ANOTHER OWNER ARE ANOTHER LINE, and come back.
	project, err := s.AddMemory(Memory{Owner: OwnerProject("alpha"), Type: MemoryFact, Text: "prefers tabs"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ForgetMemory(project.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreMemory(project.ID); err != nil {
		t.Fatalf("putting back a project's line beside the person's same words = %v", err)
	}
}

// Restore, move and reword remain available when the store exceeds the prompt budget.
func TestPutBackMoveAndRewordAreIndependentOfPromptCapacity(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	gone, err := s.AddMemory(Memory{Owner: OwnerUser, Type: MemoryFact, Text: roomLine("gone", 1)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ForgetMemory(gone.ID); err != nil {
		t.Fatal(err)
	}
	short, err := s.AddMemory(Memory{Owner: OwnerProject("alpha"), Type: MemoryFact, Text: "short"})
	if err != nil {
		t.Fatal(err)
	}
	fillTo(t, s, OwnerUser, "", "person", MemoryBlockRunes-40)

	if err := s.RestoreMemory(gone.ID); err != nil {
		t.Fatalf("putting a line back past the bound = %v", err)
	}
	if err := s.UpdateMemory(short.ID, roomLine("longer", 1)); err != nil {
		t.Fatalf("rewording a line past the bound = %v", err)
	}
	big, err := s.AddMemory(Memory{Owner: OwnerProject("gamma"), Type: MemoryFact, Text: roomLine("gamma", 1)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.MoveMemory(big.ID, OwnerMachine, ""); err != nil {
		t.Fatalf("moving a line onto every block past the bound = %v", err)
	}
}

// A MOVE OR A REPLACEMENT ONTO WORDS THE OWNER ALREADY HOLDS DOES NOT MAKE TWO
// OF THEM, and a forgotten line cannot be moved.
func TestMoveAndReplaceDoNotDuplicateTheTargetsLine(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	everywhere, _ := s.AddMemory(Memory{Owner: OwnerUser, Type: MemoryFact, Text: "uses tabs"})
	project, _ := s.AddMemory(Memory{Owner: OwnerProject("alpha"), Type: MemoryFact, Text: "Uses  tabs"})
	merged, err := s.MoveMemory(project.ID, OwnerUser, "")
	if err != nil || !merged {
		t.Fatalf("moving onto the same words = merged %v, %v", merged, err)
	}
	active, _ := s.MemoriesFor([]string{OwnerUser, OwnerProject("alpha")})
	if len(active) != 1 || active[0].ID != everywhere.ID {
		t.Fatalf("after the merge = %+v, want the line already there alone", active)
	}
	if _, err := s.MoveMemory(project.ID, OwnerMachine, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("moving a forgotten line = %v, want refused", err)
	}

	old, _ := s.AddMemory(Memory{Owner: OwnerUser, Type: MemoryFact, Text: "deploys on Fridays"})
	kept, added, err := s.SupersedeMemoryForOwners([]string{OwnerUser}, old.ID, Memory{Owner: OwnerUser, Text: "uses tabs"})
	if err != nil || added || kept.ID != everywhere.ID {
		t.Fatalf("replacing with words already kept = %+v added %v, %v", kept, added, err)
	}
	if record, _, _ := s.MemoryRecord(old.ID); record.Status == MemoryActive {
		t.Fatal("the replaced line is still active beside the line already kept")
	}
}

// WHAT RIDES WHEN LINES DO NOT ALL FIT: an earlier build's rules, then the
// project's, then the machine's, then the person's — newest first within each
// — and in the order they came in.
func TestFitMemoriesRidesRulesThenProjectThenMachineThenPerson(t *testing.T) {
	var memories []Memory
	seq := int64(0)
	add := func(owner string, rule bool, n int) {
		for i := 0; i < n; i++ {
			seq++
			memories = append(memories, Memory{ID: fmt.Sprint(owner, seq), Owner: owner, Rule: rule, Text: roomLine(owner, int(seq)), CreatedSeq: seq})
		}
	}
	add(OwnerUser, false, 60)
	add(OwnerUser, true, 1)
	add(OwnerMachine, false, 2)
	add(OwnerProject("alpha"), false, 2)
	fit, left := FitMemories(memories)
	if left == 0 {
		t.Fatal("the fixture fits; it was meant to overflow")
	}
	held := map[string]bool{}
	for _, memory := range fit {
		held[memory.ID] = true
	}
	for _, memory := range memories {
		if (memory.Rule || memory.Owner != OwnerUser) && !held[memory.ID] {
			t.Fatalf("%s (rule %v) was left out while older person lines rode", memory.ID, memory.Rule)
		}
	}
	if held[memories[0].ID] {
		t.Fatal("the oldest person line rode while newer ones were left out")
	}
	for i := 1; i < len(fit); i++ {
		if indexOf(memories, fit[i].ID) < indexOf(memories, fit[i-1].ID) {
			t.Fatal("the lines that ride came out of the order they came in")
		}
	}
}

func indexOf(memories []Memory, id string) int {
	for i, memory := range memories {
		if memory.ID == id {
			return i
		}
	}
	return -1
}

// A STORE THE PREVIOUS BUILD FILLED PAST THE BOUND KEEPS ITS RULE AND ITS
// PROJECT LINE IN FRONT OF THE MODEL. Seventy lines for the person, one for
// the project and one rule, laid down in that build's own schema with raw SQL,
// then opened by this build.
func TestAPreUpgradeStoreOverTheBoundKeepsItsRuleAndProjectLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`
CREATE TABLE memories (
    id          TEXT PRIMARY KEY,
    owner       TEXT NOT NULL DEFAULT 'user',
    type        TEXT NOT NULL,
    scope       TEXT NOT NULL,
    title       TEXT NOT NULL,
    text        TEXT NOT NULL,
    tags        TEXT NOT NULL DEFAULT '[]',
    status      TEXT NOT NULL DEFAULT 'active',
    use_count   INTEGER NOT NULL DEFAULT 0,
    miss_count  INTEGER NOT NULL DEFAULT 0,
    created_seq    INTEGER NOT NULL,
    updated_seq    INTEGER NOT NULL,
    source_session TEXT NOT NULL DEFAULT '',
    source_seq     INTEGER NOT NULL DEFAULT 0,
    always_on      INTEGER NOT NULL DEFAULT 0
)`); err != nil {
		t.Fatal(err)
	}
	insert := func(id, owner, scope, text string, always, seq int) {
		if _, err := raw.Exec(`INSERT INTO memories (id, owner, type, scope, title, text, created_seq, updated_seq, always_on)
			VALUES (?, ?, 'fact', ?, ?, ?, ?, ?, ?)`, id, owner, scope, text, text, seq, seq, always); err != nil {
			t.Fatal(err)
		}
	}
	insert("rule", OwnerUser, MemoryScopeUser, "Always run the tests before committing.", 1, 1)
	insert("project", OwnerProject("alpha"), MemoryScopeProject, "This repo deploys from staging.", 0, 2)
	for i := 0; i < 70; i++ {
		insert(fmt.Sprintf("person%02d", i), OwnerUser, MemoryScopeUser, roomLine("person", i), 0, 10+i)
	}
	_ = raw.Close()

	s := openTestStore(t, path)
	memories, err := s.MemoriesFor([]string{OwnerUser, OwnerMachine, OwnerProject("alpha")})
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) != 72 {
		t.Fatalf("the upgrade kept %d lines, want all 72", len(memories))
	}
	fit, left := FitMemories(memories)
	if left == 0 {
		t.Fatal("the fixture fits; it was meant to overflow")
	}
	rule, project := false, false
	for _, memory := range fit {
		rule = rule || memory.ID == "rule"
		project = project || memory.ID == "project"
	}
	if !rule || !project {
		t.Fatalf("the rule rode %v and the project line rode %v; both must ride before old person lines", rule, project)
	}
	// New writes are admitted without forcing the person to forget anything.
	if _, err := s.Write(WriteRequest{Owner: OwnerProject("alpha"), Type: MemoryFact, Text: "one more"}); err != nil {
		t.Fatalf("a new line on an overflowing store = %v", err)
	}
}
