package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// THE BOUND IS THE STORE'S, AND IT IS HELD INSIDE THE WRITE. Every door that
// makes a line active is measured against every block the line's owner
// reaches, in the transaction that makes it active.

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

// A LINE KEPT EVERYWHERE FROM ONE PROJECT IS MEASURED AGAINST EVERY PROJECT'S
// BLOCK: it rides beta's conversations too, so a nearly full beta refuses it —
// and names beta — while alpha's own project lines still fit.
func TestAnEverywhereLineIsMeasuredAgainstEveryProjectsBlock(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	fillTo(t, s, OwnerProject("beta"), "beta-repo", "beta", MemoryBlockRunes-60)
	_, err := s.Write(WriteRequest{Owner: OwnerUser, Type: MemoryPreference, Text: roomLine("everywhere", 1)})
	var full *MemoryFullError
	if !errors.As(err, &full) || full.Project != "beta-repo" || full.Owner != OwnerProject("beta") {
		t.Fatalf("an everywhere line that overflows beta = %v, want memory full in beta-repo", err)
	}
	if _, err := s.Write(WriteRequest{Owner: OwnerProject("alpha"), Type: MemoryFact, Text: roomLine("alpha", 1)}); err != nil {
		t.Fatalf("alpha's own line was refused for beta's room: %v", err)
	}
}

// THE SAME WORDS UNDER ANOTHER OWNER ARE ANOTHER LINE. The write door's
// duplicate rule is the target owner's, so a line already kept for a project
// still needs room when it is kept everywhere.
func TestADuplicateUnderAnotherOwnerStillNeedsRoom(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	text := roomLine("shared", 1)
	if _, err := s.AddMemory(Memory{Owner: OwnerProject("alpha"), Type: MemoryFact, Text: text}); err != nil {
		t.Fatal(err)
	}
	fillTo(t, s, OwnerUser, "", "person", MemoryBlockRunes-60)
	if _, err := s.Write(WriteRequest{Owner: OwnerUser, Type: MemoryFact, Text: text}); !errors.Is(err, ErrMemoryFull) {
		t.Fatalf("the same words for another owner skipped the bound: %v", err)
	}
	// AND THE SAME WORDS FOR THE SAME OWNER NEED NONE: that is a skip.
	if got, err := s.Write(WriteRequest{Owner: OwnerProject("alpha"), Type: MemoryFact, Text: text}); err != nil || got.Outcome != WriteOutcomeSkipped {
		t.Fatalf("a duplicate in its own owner = %+v, %v; want a skip that needs no room", got, err)
	}
}

// TWO WRITERS WITH ROOM FOR ONE ADMIT ONE. The measure is inside the write's
// immediate transaction, so the second writer reads the first one's line.
func TestTwoWritersWithRoomForOneAdmitOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	first := openTestStore(t, path)
	second := openTestStore(t, path)
	used := fillTo(t, first, OwnerUser, "", "person", MemoryBlockRunes-200)
	room := MemoryBlockRunes - used
	text := func(who string) string {
		// One line that fits the room, and two that do not fit together.
		body := who + " " + strings.Repeat("r", room/2+20)
		for MemoryLineCost(body) > room {
			body = body[:len(body)-1]
		}
		return body
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, s := range []*Store{first, second} {
		wg.Add(1)
		go func(i int, s *Store) {
			defer wg.Done()
			_, errs[i] = s.Write(WriteRequest{Owner: OwnerUser, Type: MemoryFact, Text: text(fmt.Sprint("writer", i))})
		}(i, s)
	}
	wg.Wait()
	ok, full := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrMemoryFull):
			full++
		default:
			t.Fatalf("a writer failed for another reason: %v", err)
		}
	}
	if ok != 1 || full != 1 {
		t.Fatalf("two writers with room for one: %d admitted, %d refused (%v)", ok, full, errs)
	}
}

// A SECOND WRITER READS THE ROOM ONLY AFTER THE FIRST HAS COMMITTED. The check
// runs inside the write's own immediate transaction, so while one writer holds
// its answer the other cannot even ask. Here the first is held inside its
// transaction, past its check: the second must not reach its own check until
// the first lets go, and then it finds no room. A check read outside the lock
// would let both through, which is the race the other test can only hope to
// land on.
func TestASecondWriterReadsTheRoomOnlyAfterTheFirstCommits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	first := openTestStore(t, path)
	second := openTestStore(t, path)
	used := fillTo(t, first, OwnerUser, "", "person", MemoryBlockRunes-200)
	room := MemoryBlockRunes - used
	line := func(who string) string {
		body := who + " " + strings.Repeat("r", room/2+20)
		for MemoryLineCost(body) > room {
			body = body[:len(body)-1]
		}
		return body
	}
	read := make(chan string, 4)
	release := make(chan struct{})
	memoryRoomRead = func(text string, _ error) {
		read <- text
		if strings.HasPrefix(text, "first") {
			<-release
		}
	}
	t.Cleanup(func() { memoryRoomRead = nil })

	var wg sync.WaitGroup
	var firstErr, secondErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, firstErr = first.Write(WriteRequest{Owner: OwnerUser, Type: MemoryFact, Text: line("first")})
	}()
	if got := <-read; !strings.HasPrefix(got, "first") {
		t.Fatalf("the first room check was %q", got)
	}
	go func() {
		defer wg.Done()
		_, secondErr = second.Write(WriteRequest{Owner: OwnerUser, Type: MemoryFact, Text: line("second")})
	}()
	select {
	case got := <-read:
		close(release)
		wg.Wait()
		t.Fatalf("the second writer read the room while the first's line was uncommitted (%q)", got)
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	if firstErr != nil {
		t.Fatalf("the first writer, which had room, was refused: %v", firstErr)
	}
	if !errors.Is(secondErr, ErrMemoryFull) {
		t.Fatalf("the second writer, reading after the first committed, = %v; want memory full", secondErr)
	}
}

// A REPLACEMENT IS MEASURED TOO: a correction longer than the line it replaces
// is refused when the block has no room for the difference, and the line it
// would have replaced is still kept.
func TestAReplacementIsMeasured(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	old, err := s.AddMemory(Memory{Owner: OwnerUser, Type: MemoryFact, Text: "short"})
	if err != nil {
		t.Fatal(err)
	}
	// Between 105 and 200 runes of room are left: never enough for 250 more.
	fillTo(t, s, OwnerUser, "", "person", MemoryBlockRunes-200)
	owners := []string{OwnerUser, OwnerMachine}
	_, _, err = s.SupersedeMemoryForOwners(owners, old.ID, Memory{Owner: OwnerUser, Type: MemoryFact, Text: strings.Repeat("longer ", 36)})
	if !errors.Is(err, ErrMemoryFull) {
		t.Fatalf("a longer replacement past the bound = %v", err)
	}
	active, err := s.MemoriesFor([]string{OwnerUser})
	if err != nil {
		t.Fatal(err)
	}
	if indexOf(active, old.ID) < 0 {
		t.Fatal("a refused replacement let the old line go")
	}
	// AND ONE NO LONGER THAN WHAT IT REPLACES ALWAYS FITS: the old line's room
	// is the new one's.
	if _, added, err := s.SupersedeMemoryForOwners(owners, old.ID, Memory{Owner: OwnerUser, Type: MemoryFact, Text: "brief"}); err != nil || !added {
		t.Fatalf("a replacement the same size = %v, %v", added, err)
	}
}

// THE LEGACY IMPORT IS THE ONE WRITE THE BOUND DOES NOT MEASURE: those lines
// were the person's before the bound existed, and refusing them would lose
// them. Every other write is still refused once the block is past it.
func TestAnUnboundedWriteIsKeptPastTheBound(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	fillTo(t, s, OwnerUser, "", "person", MemoryBlockRunes-40)
	got, err := s.Write(WriteRequest{Owner: OwnerUser, Type: MemoryFact, Text: roomLine("imported", 1), Unbounded: true})
	if err != nil || got.Outcome == WriteOutcomeSkipped {
		t.Fatalf("an unbounded write past the bound = %+v, %v; want it kept", got, err)
	}
	if _, err := s.Write(WriteRequest{Owner: OwnerUser, Type: MemoryFact, Text: roomLine("after", 1)}); !errors.Is(err, ErrMemoryFull) {
		t.Fatalf("a measured write after it = %v; want memory full", err)
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

// PUT BACK, MOVE AND REWORD MAKE A LINE ACTIVE OR BIGGER, and each is measured.
func TestPutBackMoveAndRewordAreMeasured(t *testing.T) {
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

	if err := s.RestoreMemory(gone.ID); !errors.Is(err, ErrMemoryFull) {
		t.Fatalf("putting a line back past the bound = %v", err)
	}
	if err := s.UpdateMemory(short.ID, roomLine("longer", 1)); !errors.Is(err, ErrMemoryFull) {
		t.Fatalf("rewording a line past the bound = %v", err)
	}
	big, err := s.AddMemory(Memory{Owner: OwnerProject("gamma"), Type: MemoryFact, Text: roomLine("gamma", 1)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.MoveMemory(big.ID, OwnerMachine, ""); !errors.Is(err, ErrMemoryFull) {
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
	// AND A NEW LINE IS REFUSED UNTIL SOME ARE FORGOTTEN.
	if _, err := s.Write(WriteRequest{Owner: OwnerProject("alpha"), Type: MemoryFact, Text: "one more"}); !errors.Is(err, ErrMemoryFull) {
		t.Fatalf("a new line on an overflowing store = %v", err)
	}
}
