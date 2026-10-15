package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

// ── rules: memories marked always ──────────────────────────────────────────

// alwaysEvents counts the rule events the journal holds, which is how a test
// says "this wrote history" and "this wrote nothing" without trusting the door
// that wrote it.
func alwaysEvents(t *testing.T, graph *Store) int {
	t.Helper()
	var count int
	if err := graph.db.QueryRow(`SELECT COUNT(*) FROM events WHERE kind = ?`, EventMemoryAlways).Scan(&count); err != nil {
		t.Fatalf("count rule events: %v", err)
	}
	return count
}

// AN OLD STORE OPENS WITH ITS RULES COLUMN AND EVERY NOTE AS IT WAS. The fixture
// is the released table with no owner and no always_on, laid down by raw SQL so
// nothing in this build can repair it first; and the same again for a store an
// earlier upgrade already gave its owners.
func TestAStoreFromBeforeRulesOpensWithTheColumnAndNoRules(t *testing.T) {
	for name, prepare := range map[string]func(t *testing.T, path string){
		"before owners": func(t *testing.T, path string) { writeLegacyDatabase(t, path) },
		"after owners, before rules": func(t *testing.T, path string) {
			writeLegacyDatabase(t, path)
			addLegacyOwnerColumn(t, path, true)
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "legacy.db")
			prepare(t, path)
			graph := openTestStore(t, path)
			if columns := memoriesTableColumns(t, graph); !columns["always_on"] {
				t.Fatalf("the open did not add the rules column: %v", columns)
			}
			if !indexOnMemories(t, graph, "memories_always_active") {
				t.Fatal("the open did not leave the rules index on memories")
			}
			for _, want := range legacyMemoryRows() {
				got, ok, err := graph.MemoryRecord(want.id)
				if err != nil || !ok {
					t.Fatalf("read %s after the upgrade: (%v, %v, %v)", want.id, got, ok, err)
				}
				if got.Always {
					t.Errorf("%s came out of the upgrade as a rule nobody set", want.id)
				}
				if got.Text != want.text || got.Status != want.status {
					t.Errorf("%s = (%q, %q), want (%q, %q)", want.id, got.Text, got.Status, want.text, want.status)
				}
			}
			rules, err := graph.AlwaysMemories([]string{OwnerUser, OwnerMachine}, 0)
			if err != nil || len(rules) != 0 {
				t.Fatalf("rules on an upgraded store = (%v, %v), want none", memoryIDs(rules), err)
			}
			// And an old line can become a rule now.
			if err := graph.SetMemoryAlways("mem_user", true); err != nil {
				t.Fatalf("make an upgraded line a rule: %v", err)
			}
			rules, err = graph.AlwaysMemories([]string{OwnerUser}, 0)
			if err != nil || len(rules) != 1 || rules[0].ID != "mem_user" {
				t.Fatalf("rules after setting one = (%v, %v), want mem_user", memoryIDs(rules), err)
			}
		})
	}
}

// TWO PROCESSES OPENING ONE OLD STORE AT ONCE BOTH OPEN IT. The column checks
// are read inside the immediate transactions that alter the table, so the
// second open waits for the first and finds the columns it made instead of
// dying on a duplicate column.
//
// THE FIXTURE IS PUT IN WAL FIRST, because every store a released build wrote
// already is: [Open] switches the journal mode on its first open, and two opens
// of a file still in rollback mode race on that switch, which is a question
// about a raw fixture and not about any store on disk.
func TestTwoOpensOfAnOldStoreAtOnceBothSucceed(t *testing.T) {
	for round := 0; round < 5; round++ {
		path := filepath.Join(t.TempDir(), "race.db")
		writeLegacyDatabase(t, path)
		raw, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := raw.Exec(`PRAGMA journal_mode=WAL`); err != nil {
			t.Fatalf("put the fixture in WAL: %v", err)
		}
		_ = raw.Close()
		const opens = 4
		var wg sync.WaitGroup
		errs := make([]error, opens)
		stores := make([]*Store, opens)
		for i := 0; i < opens; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				stores[i], errs[i] = Open(path)
			}(i)
		}
		wg.Wait()
		for i := 0; i < opens; i++ {
			if errs[i] != nil {
				t.Fatalf("round %d: open %d of one old store failed: %v", round, i, errs[i])
			}
			t.Cleanup(func() { _ = stores[i].Close() })
		}
		if columns := memoriesTableColumns(t, stores[0]); !columns["always_on"] || !columns["owner"] {
			t.Fatalf("round %d: columns after concurrent opens = %v", round, columns)
		}
	}
}

// THE SAME WORDS ASKED FOR ALWAYS PROMOTE THE LINE THAT WAS THERE: one row, the
// same id, a rule now, and one event saying so.
func TestAnAlwaysWritePromotesTheRememberedCopy(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "promote.db"))
	owner := OwnerProject("tabs")
	recalled, err := graph.Write(WriteRequest{Owner: owner, Type: MemoryPreference,
		Title: "Tabs", Text: "Always indent with tabs in this repository."})
	if err != nil || recalled.Outcome != WriteOutcomeAdded || recalled.Memory.Always {
		t.Fatalf("the ordinary write = (%+v, %v)", recalled, err)
	}
	promoted, err := graph.Write(WriteRequest{Owner: owner, Type: MemoryPreference,
		Title: "Tabs please", Text: "always indent with   TABS in this repository.", Always: true})
	if err != nil {
		t.Fatalf("the always write: %v", err)
	}
	if promoted.Outcome != WriteOutcomePromoted || promoted.Memory.ID != recalled.Memory.ID || !promoted.Memory.Always {
		t.Fatalf("the always write = %+v, want the same row promoted", promoted)
	}
	rows, err := graph.ListMemories([]string{owner}, 0)
	if err != nil || len(rows) != 1 || !rows[0].Always {
		t.Fatalf("held rows = (%+v, %v), want one rule", rows, err)
	}
	if rows[0].UpdatedSeq != recalled.Memory.UpdatedSeq {
		t.Errorf("promotion moved updated_seq %d → %d; a rule is the same memory", recalled.Memory.UpdatedSeq, rows[0].UpdatedSeq)
	}
	if got := alwaysEvents(t, graph); got != 1 {
		t.Errorf("rule events = %d, want the one promotion", got)
	}
	// And asking again is a skip that writes no second event.
	again, err := graph.Write(WriteRequest{Owner: owner, Type: MemoryPreference,
		Title: "Tabs", Text: "Always indent with tabs in this repository.", Always: true})
	if err != nil || again.Outcome != WriteOutcomeSkipped || !again.Memory.Always {
		t.Fatalf("a second always write = (%+v, %v), want a skip of the rule", again, err)
	}
	if got := alwaysEvents(t, graph); got != 1 {
		t.Errorf("rule events after a repeat = %d, want still one", got)
	}
}

// AN ORDINARY WRITE NEVER TAKES A RULE BACK. The person said always once; the
// same words mentioned in passing — or noticed by the post-turn pass — leave
// the rule a rule.
func TestARememberedWriteNeverDemotesARule(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "demote.db"))
	rule, err := graph.Write(WriteRequest{Owner: OwnerUser, Type: MemoryPreference,
		Title: "No force pushes", Text: "Never force-push a shared branch.", Always: true})
	if err != nil || rule.Outcome != WriteOutcomeAdded || !rule.Memory.Always {
		t.Fatalf("the rule = (%+v, %v)", rule, err)
	}
	again, err := graph.Write(WriteRequest{Owner: OwnerUser, Type: MemoryPreference,
		Title: "Force pushes", Text: "never force-push a shared branch."})
	if err != nil || again.Outcome != WriteOutcomeSkipped || again.Memory.ID != rule.Memory.ID {
		t.Fatalf("the ordinary repeat = (%+v, %v), want a skip of the rule", again, err)
	}
	got, ok, err := graph.MemoryRecord(rule.Memory.ID)
	if err != nil || !ok || !got.Always {
		t.Fatalf("the rule after an ordinary repeat = (%+v, %v, %v), want still always", got, ok, err)
	}
}

// THE RULES ARE READ IN ONE ORDER, FOR THE OWNERS NAMED AND NO OTHERS: the
// person's first, then this machine's, then the project's, each oldest first;
// never another project's, never the quarantine, never a line let go of, never
// a line that is merely remembered.
func TestAlwaysMemoriesReadsTheOwnersRulesInOneOrder(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "order.db"))
	here, there := OwnerProject("here"), OwnerProject("there")
	write := func(owner, text string, always bool) Memory {
		t.Helper()
		result, err := graph.Write(WriteRequest{Owner: owner, Type: MemoryPreference, Title: text, Text: text, Always: always})
		if err != nil {
			t.Fatalf("write %q: %v", text, err)
		}
		return result.Memory
	}
	projectOld := write(here, "project rule, oldest", true)
	userOld := write(OwnerUser, "user rule, oldest", true)
	machine := write(OwnerMachine, "machine rule", true)
	write(there, "another project's rule", true)
	write(here, "a remembered line, not a rule", false)
	userNew := write(OwnerUser, "user rule, newest", true)
	gone := write(OwnerUser, "a rule let go of", true)
	if err := graph.ForgetMemory(gone.ID); err != nil {
		t.Fatal(err)
	}
	projectNew := write(here, "project rule, newest", true)
	// A row in quarantine cannot be made a rule through any door.
	quarantined := mustAddMemory(t, graph, Memory{Type: MemoryPreference, Scope: MemoryScopeProject,
		Title: "quarantined", Text: "A line nobody can prove the project of.", Always: true})
	if quarantined.Always {
		t.Fatalf("a quarantined row landed as a rule: %+v", quarantined)
	}
	if err := graph.SetMemoryAlways(quarantined.ID, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("making a quarantined row a rule = %v, want a refusal", err)
	}

	owners := []string{here, OwnerMachine, OwnerUser, OwnerLegacyProject}
	rules, err := graph.AlwaysMemories(owners, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{userOld.ID, userNew.ID, machine.ID, projectOld.ID, projectNew.ID}
	if got := memoryIDs(rules); !reflect.DeepEqual(got, want) {
		t.Fatalf("rules = %v, want %v", got, want)
	}
	for _, rule := range rules {
		if !rule.Always {
			t.Errorf("%s came back from the rules read without its flag", rule.ID)
		}
	}
	limited, err := graph.AlwaysMemories(owners, 2)
	if err != nil || !reflect.DeepEqual(memoryIDs(limited), want[:2]) {
		t.Fatalf("rules capped at two = (%v, %v), want the two oldest of the person's", memoryIDs(limited), err)
	}
	if _, err := graph.AlwaysMemories(nil, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("the rules read accepted no owners: %v", err)
	}
	if rules, err := graph.AlwaysMemories([]string{OwnerLegacyProject}, 0); err != nil || len(rules) != 0 {
		t.Fatalf("the quarantine's rules = (%v, %v), want none", memoryIDs(rules), err)
	}
}

// A RULE IS NEVER A ROUTER CANDIDATE: it is already in front of every turn, and
// a shortlist place spent on it is a place a remembered line did not get.
func TestARuleIsNotOfferedToTheRouter(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "candidates.db"))
	rule, err := graph.Write(WriteRequest{Owner: OwnerUser, Type: MemoryPreference,
		Title: "Tabs", Text: "Indent the deploy scripts with tabs.", Always: true})
	if err != nil {
		t.Fatal(err)
	}
	line, err := graph.Write(WriteRequest{Owner: OwnerUser, Type: MemoryFact,
		Title: "Deploy box", Text: "The deploy scripts run on the amber box."})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := graph.MemoryCandidates([]string{OwnerUser}, "deploy scripts tabs", MemoryCandidatesDefault)
	if err != nil {
		t.Fatal(err)
	}
	if containsMemoryStub(pool, rule.Memory.ID) {
		t.Errorf("the rule is in the router's shortlist: %+v", pool)
	}
	if !containsMemoryStub(pool, line.Memory.ID) {
		t.Errorf("the remembered line is missing from the shortlist: %+v", pool)
	}
	// The flag cleared, it is an ordinary line again and the router may see it.
	if err := graph.SetMemoryAlways(rule.Memory.ID, false); err != nil {
		t.Fatal(err)
	}
	pool, err = graph.MemoryCandidates([]string{OwnerUser}, "deploy scripts tabs", MemoryCandidatesDefault)
	if err != nil || !containsMemoryStub(pool, rule.Memory.ID) {
		t.Fatalf("after the rule was taken back the shortlist = (%+v, %v)", pool, err)
	}
}

// THE DOORS SAY NO TO WHAT CANNOT BE A RULE, AND A FLAG THAT DID NOT MOVE WRITES
// NOTHING. Nor does a toggle move the row's place in time.
func TestSettingARuleRefusesWhatCannotBeOneAndWritesOnlyChanges(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "toggle.db"))
	mine, err := graph.Write(WriteRequest{Owner: OwnerProject("mine"), Type: MemoryFact, Title: "Mine", Text: "This project ships weekly."})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.SetMemoryAlwaysForOwners([]string{OwnerProject("theirs")}, mine.Memory.ID, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("another project's door made this row a rule: %v", err)
	}
	if err := graph.SetMemoryAlwaysForOwners(nil, mine.Memory.ID, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("the owners door accepted no owners: %v", err)
	}
	if err := graph.SetMemoryAlways("", true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an empty id was accepted: %v", err)
	}
	if err := graph.SetMemoryAlways("mem_nothing", true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a missing id was accepted: %v", err)
	}
	if err := graph.SetMemoryAlwaysForOwners([]string{OwnerProject("mine")}, mine.Memory.ID, true); err != nil {
		t.Fatalf("the owner's own door: %v", err)
	}
	if err := graph.SetMemoryAlways(mine.Memory.ID, true); err != nil {
		t.Fatalf("setting a rule that already stands: %v", err)
	}
	if got := alwaysEvents(t, graph); got != 1 {
		t.Fatalf("rule events = %d, want one: an unchanged flag is not history", got)
	}
	row, _, err := graph.MemoryRecord(mine.Memory.ID)
	if err != nil || !row.Always || row.UpdatedSeq != mine.Memory.UpdatedSeq {
		t.Fatalf("the rule = (%+v, %v), want always with updated_seq %d untouched", row, err, mine.Memory.UpdatedSeq)
	}
	if err := graph.ForgetMemory(mine.Memory.ID); err != nil {
		t.Fatal(err)
	}
	if err := graph.SetMemoryAlways(mine.Memory.ID, false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a forgotten line's flag was changed: %v", err)
	}
}

// A RULE SAID BETTER IS STILL A RULE: both supersede doors carry the retired
// row's flag onto the replacement, and a replacement that asked for always is
// one even when the line it retires was not.
func TestASupersessionKeepsTheRule(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "supersede.db"))
	owner := OwnerProject("keep")
	rule, err := graph.Write(WriteRequest{Owner: owner, Type: MemoryPreference, Title: "Tests", Text: "Run the unit tests before committing.", Always: true})
	if err != nil {
		t.Fatal(err)
	}
	better, err := graph.SupersedeMemoryForOwners([]string{owner}, rule.Memory.ID, Memory{Owner: owner, Type: MemoryPreference,
		Title: "Tests", Text: "Run the unit and race tests before committing."})
	if err != nil || !better.Always {
		t.Fatalf("the owners-door replacement = (%+v, %v), want a rule", better, err)
	}
	best, err := graph.SupersedeMemory(better.ID, Memory{Owner: owner, Type: MemoryPreference,
		Title: "Tests", Text: "Run every test before committing."})
	if err != nil || !best.Always {
		t.Fatalf("the raw-door replacement = (%+v, %v), want a rule", best, err)
	}
	line, err := graph.Write(WriteRequest{Owner: owner, Type: MemoryFact, Title: "Lint", Text: "Lint runs in CI."})
	if err != nil {
		t.Fatal(err)
	}
	asked, err := graph.SupersedeMemoryForOwners([]string{owner}, line.Memory.ID, Memory{Owner: owner, Type: MemoryPreference,
		Title: "Lint", Text: "Run the linter before every commit.", Always: true})
	if err != nil || !asked.Always {
		t.Fatalf("a replacement that asked for always = (%+v, %v), want a rule", asked, err)
	}
	rules, err := graph.AlwaysMemories([]string{owner}, 0)
	if err != nil || !reflect.DeepEqual(memoryIDs(rules), []string{best.ID, asked.ID}) {
		t.Fatalf("rules = (%v, %v), want the two replacements", memoryIDs(rules), err)
	}
}

// A REBUILD LANDS EVERY RULE WHERE IT WAS: one born a rule, one promoted, one
// set and then taken back, and one carried through a supersession.
func TestRebuildReplaysTheRules(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "rebuild.db"))
	born, err := graph.Write(WriteRequest{Owner: OwnerUser, Type: MemoryPreference, Title: "Born", Text: "Write commit messages in the imperative.", Always: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.Write(WriteRequest{Owner: OwnerMachine, Type: MemoryFact, Title: "Promoted", Text: "Builds on this machine use the local cache."}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.Write(WriteRequest{Owner: OwnerMachine, Type: MemoryFact, Title: "Promoted", Text: "Builds on this machine use the local cache.", Always: true}); err != nil {
		t.Fatal(err)
	}
	back, err := graph.Write(WriteRequest{Owner: OwnerUser, Type: MemoryPreference, Title: "Back", Text: "Prefer short answers.", Always: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.SetMemoryAlways(back.Memory.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.SupersedeMemory(born.Memory.ID, Memory{Owner: OwnerUser, Type: MemoryPreference, Title: "Born", Text: "Write every commit message in the imperative."}); err != nil {
		t.Fatal(err)
	}
	before := memoryRows(t, graph)
	rulesBefore, err := graph.AlwaysMemories([]string{OwnerUser, OwnerMachine}, 0)
	if err != nil || len(rulesBefore) != 2 {
		t.Fatalf("rules before the rebuild = (%v, %v), want two", memoryIDs(rulesBefore), err)
	}
	if _, err := graph.db.Exec(`DELETE FROM memories_fts`); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.db.Exec(`DELETE FROM memories`); err != nil {
		t.Fatal(err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if after := memoryRows(t, graph); !reflect.DeepEqual(after, before) {
		t.Fatalf("memories after rebuild = %+v, want %+v", after, before)
	}
	rulesAfter, err := graph.AlwaysMemories([]string{OwnerUser, OwnerMachine}, 0)
	if err != nil || !reflect.DeepEqual(memoryIDs(rulesAfter), memoryIDs(rulesBefore)) {
		t.Fatalf("rules after rebuild = (%v, %v), want %v", memoryIDs(rulesAfter), err, memoryIDs(rulesBefore))
	}
}

// A RULE TRAVELS WITH ITS WORDS. A receiver folding another store's journal
// lands the rules as rules — set at birth and set later — and a re-delivery
// changes nothing.
func TestARuleFoldsAcrossStores(t *testing.T) {
	origin := openTestStore(t, filepath.Join(t.TempDir(), "origin.db"))
	receiver := openTestStore(t, filepath.Join(t.TempDir(), "receiver.db"))
	born, err := origin.Write(WriteRequest{Owner: OwnerUser, Type: MemoryPreference, Title: "Born", Text: "Answer in British English.", Always: true})
	if err != nil {
		t.Fatal(err)
	}
	later, err := origin.Write(WriteRequest{Owner: OwnerUser, Type: MemoryFact, Title: "Later", Text: "The staging box is called amber."})
	if err != nil {
		t.Fatal(err)
	}
	if err := origin.SetMemoryAlways(later.Memory.ID, true); err != nil {
		t.Fatal(err)
	}
	events, _, _, err := origin.ExportMemoryEvents(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.ApplyMemoryEvents(events, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	rules, err := receiver.AlwaysMemories([]string{OwnerUser}, 0)
	if err != nil || !reflect.DeepEqual(memoryIDs(rules), []string{born.Memory.ID, later.Memory.ID}) {
		t.Fatalf("the receiver's rules = (%v, %v), want both", memoryIDs(rules), err)
	}
	again, err := receiver.ApplyMemoryEvents(events, nil)
	if err != nil || again.Applied != 0 {
		t.Fatalf("a re-delivery = (%+v, %v), want nothing applied", again, err)
	}
}

// THE RULES MIGRATION, RUN BY SEVERAL PROCESSES AT ONCE, ALTERS THE TABLE ONCE.
// Each handle is opened the way [Open] opens one — immediate transactions and
// SQLite's own patience — and all of them run the migration together on a store
// that has its owners and no rules column yet. A check read on the pool before
// the transaction would let every one of them read "absent" and all but the
// first die on a duplicate column; read inside it, each waits its turn and the
// later ones find the column already made.
func TestTheRulesMigrationRunConcurrentlyAltersOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migrate.db")
	writeLegacyDatabase(t, path)
	addLegacyOwnerColumn(t, path, true)
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()

	const handles = 8
	dbs := make([]*sql.DB, handles)
	for i := range dbs {
		u := url.URL{Scheme: "file", Path: path}
		query := u.Query()
		query.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyWait.Milliseconds()))
		query.Set("_txlock", "immediate")
		u.RawQuery = query.Encode()
		db, err := sql.Open("sqlite", u.String())
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		if err := db.Ping(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		dbs[i] = db
	}
	start := make(chan struct{})
	errs := make([]error, handles)
	var wg sync.WaitGroup
	for i := range dbs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = migrateMemoriesAlways(dbs[i])
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("migration %d of %d failed: %v", i, handles, err)
		}
	}
	found, err := tableHasColumn(dbs[0], "memories", "always_on")
	if err != nil || !found {
		t.Fatalf("the rules column after the migrations = (%v, %v)", found, err)
	}
}
