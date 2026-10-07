package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ── owner isolation ─────────────────────────────────────────────────────────

// THE OWNER IS THE PERMISSION MODEL. A memory written under one project's
// owner must be invisible to every read that names another: no injection, no
// dedup answer, no forget match. Two projects may hold genuinely different
// truths about the same words, and the reads are what keep them apart.
func TestAMemoryWrittenUnderOneOwnerIsInvisibleToAnother(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "isolation.db"))
	alpha := OwnerProject("alpha")
	beta := OwnerProject("beta")

	alphaRow, err := graph.Write(WriteRequest{
		Owner: alpha, Type: MemoryDecision,
		Title: "Deploys to the amber cluster", Text: "Deploys to the amber cluster every Tuesday.",
		SourceSession: "session-a",
	})
	if err != nil || alphaRow.Outcome != WriteOutcomeAdded {
		t.Fatalf("write into alpha = (%+v, %v)", alphaRow, err)
	}

	// The router's shortlist under beta has nothing from alpha.
	pool, err := graph.MemoryCandidates([]string{beta}, "amber cluster deploy", MemoryCandidatesDefault)
	if err != nil {
		t.Fatalf("MemoryCandidates under beta: %v", err)
	}
	if containsMemoryStub(pool, alphaRow.Memory.ID) {
		t.Errorf("alpha's memory is in beta's pool: %+v", pool)
	}
	// A search under beta finds nothing of alpha's.
	hits, err := graph.SearchMemories([]string{beta}, "amber cluster", 5)
	if err != nil || len(hits) != 0 {
		t.Fatalf("search under beta = (%v, %v), want nothing of alpha's", memoryIDs(hits), err)
	}
	// A get by id — a pointer that survived in a transcript — hands back nothing.
	got, err := graph.GetMemories([]string{beta}, []string{alphaRow.Memory.ID})
	if err != nil || len(got) != 0 {
		t.Fatalf("GetMemories under beta = (%v, %v), want the pointer refused", memoryIDs(got), err)
	}
	// And the owner who wrote it still sees it.
	theirs, err := graph.SearchMemories([]string{alpha}, "amber cluster", 5)
	if err != nil || len(theirs) != 1 || theirs[0].ID != alphaRow.Memory.ID {
		t.Fatalf("search under alpha = (%v, %v), want its own row", memoryIDs(theirs), err)
	}
}

// An empty owners list is refused on every read that can put a memory in front
// of a model. "Unscoped" was the scope leak; the refusal is what keeps it
// closed even for a caller that forgot to name its owner.
func TestOwnerReadsRefuseAnEmptyOwnersList(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "unscoped.db"))
	mustAddMemory(t, graph, Memory{Owner: OwnerUser, Type: MemoryFact, Title: "Fine", Text: "Fine."})
	for name, call := range map[string]func() error{
		"MemoryCandidates": func() error { _, err := graph.MemoryCandidates(nil, "fine", 8); return err },
		"SearchMemories":   func() error { _, err := graph.SearchMemories(nil, "fine", 8); return err },
		"GetMemories":      func() error { _, err := graph.GetMemories(nil, []string{"mem_x"}); return err },
		"MemoryPage":       func() error { _, _, err := graph.MemoryPage(nil, MemoryCursor{}, 8); return err },
	} {
		if err := call(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s accepted an empty owners list: %v", name, err)
		}
	}
	// ListMemories stays the janitor's read: empty means every owner, by
	// contract, because its work is by id and never a retrieval.
	all, err := graph.ListMemories(nil, 0)
	if err != nil || len(all) != 1 {
		t.Fatalf("the janitor's list = (%v, %v), want the row", memoryIDs(all), err)
	}
}

// The write door refuses an owner it cannot prove, and it is the reason the
// quarantine exists: a caller that cannot NAME a project cannot put a row into
// one.
func TestWriteRefusesAnOwnerItCannotProve(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "refuse.db"))
	for name, req := range map[string]WriteRequest{
		"empty owner":  {Type: MemoryFact, Title: "T", Text: "Body."},
		"vague owner":  {Owner: "project", Type: MemoryFact, Title: "T", Text: "Body."},
		"junk owner":   {Owner: "project:", Type: MemoryFact, Title: "T", Text: "Body."},
		"made-up kind": {Owner: "team:the-fold", Type: MemoryFact, Title: "T", Text: "Body."},
	} {
		if _, err := graph.Write(req); !errors.Is(err, ErrInvalid) {
			t.Errorf("Write(%s) = %v, want an ErrInvalid refusal", name, err)
		}
	}
	// Nothing was written.
	rows, err := graph.ListMemories(nil, 0)
	if err != nil || len(rows) != 0 {
		t.Fatalf("rows after refusals = (%v, %v), want an empty store", memoryIDs(rows), err)
	}
}

// ── the one write door ──────────────────────────────────────────────────────

// A repeated "remember this" through the one door skips the second: exact
// words within the same owner, and the skip is journaled.
func TestWriteDoorSkipsADuplicateWithinTheSameOwner(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "door.db"))
	alpha := OwnerProject("alpha")

	first, err := graph.Write(WriteRequest{Owner: alpha, Type: MemoryPreference,
		Title: "Prefers tabs", Text: "Prefers tabs over spaces in Go files."})
	if err != nil || first.Outcome != WriteOutcomeAdded {
		t.Fatalf("first write = (%+v, %v)", first, err)
	}
	// THE SAME WORDS, RETYPED — different case and spacing are the same words.
	again, err := graph.Write(WriteRequest{Owner: alpha, Type: MemoryPreference,
		Title: "Prefers tabs", Text: "PREFERS   tabs over spaces in Go files."})
	if err != nil || again.Outcome != WriteOutcomeSkipped {
		t.Fatalf("repeated write = (%+v, %v), want a skip", again, err)
	}
	if again.Memory.ID != first.Memory.ID {
		t.Fatalf("skip names %q, want the row it matched %q", again.Memory.ID, first.Memory.ID)
	}
	// THE SAME TITLE MAY LABEL DIFFERENT FACTS. Only the body determines
	// an exact duplicate, so the changed words remain available for review.
	retitled, err := graph.Write(WriteRequest{Owner: alpha, Type: MemoryFact,
		Title: "prefers TABS", Text: "An entirely different sentence about editors."})
	if err != nil || retitled.Outcome != WriteOutcomeAdded {
		t.Fatalf("same-title write = (%+v, %v), want an add", retitled, err)
	}

	// The other owner is NOT deduplicated against: beta may hold a genuinely
	// different truth about tabs.
	beta, err := graph.Write(WriteRequest{Owner: OwnerProject("beta"), Type: MemoryPreference,
		Title: "Prefers tabs", Text: "Prefers tabs over spaces in Go files."})
	if err != nil || beta.Outcome != WriteOutcomeAdded {
		t.Fatalf("write into beta = (%+v, %v), want an add — the owners are different", beta, err)
	}

	// Both outcomes are journaled.
	skips, err := graph.db.Query(`SELECT COUNT(*) FROM events WHERE kind = ?`, EventMemorySkipped)
	if err != nil {
		t.Fatal(err)
	}
	defer skips.Close()
	var count int
	for skips.Next() {
		if err := skips.Scan(&count); err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 {
		t.Fatalf("skips journaled = %d, want 1", count)
	}
}

// The dedup fallback holds on a store whose FTS index never existed: the same
// door, the same rule, a bounded scan instead of the index.
func TestWriteDoorDeduplicatesWithoutTheIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "noindex.db")
	graph := openTestStore(t, path)
	// Retire the index and tell the handle, the way a probe-failed open would
	// have: the degraded shape is store.fts = false with no index table.
	if _, err := graph.db.Exec(`DROP TABLE memories_fts`); err != nil {
		t.Fatal(err)
	}
	graph.fts = false

	first, err := graph.Write(WriteRequest{Owner: OwnerUser, Type: MemoryFact,
		Title: "Amber key", Text: "The amber key opens the archive."})
	if err != nil || first.Outcome != WriteOutcomeAdded {
		t.Fatalf("first write without an index = (%+v, %v)", first, err)
	}
	again, err := graph.Write(WriteRequest{Owner: OwnerUser, Type: MemoryFact,
		Title: "Amber key", Text: "The AMBER key opens the archive."})
	if err != nil || again.Outcome != WriteOutcomeSkipped {
		t.Fatalf("repeat without an index = (%+v, %v), want a skip", again, err)
	}
	// And the degraded search answers nil — a miss, not an error.
	hits, err := graph.SearchMemories([]string{OwnerUser}, "amber", 5)
	if err != nil || hits != nil {
		t.Fatalf("search without an index = (%v, %v), want (nil, nil)", hits, err)
	}
	// The no-index candidates pool still ranks, on the arithmetic tiers only.
	pool, err := graph.MemoryCandidates([]string{OwnerUser}, "amber", 8)
	if err != nil || len(pool) != 1 {
		t.Fatalf("pool without an index = (%+v, %v), want the row on recency alone", pool, err)
	}
}

// ── the failure journal ─────────────────────────────────────────────────────

// A write failure that reaches the journal is readable, with the door that
// failed and the reason. Errors at these doors used to vanish.
func TestJournalMemoryFailureIsReadable(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "failures.db"))
	if err := graph.JournalMemoryFailure("extract", errors.New("the decider answered nonsense")); err != nil {
		t.Fatalf("JournalMemoryFailure: %v", err)
	}
	if err := graph.JournalMemoryFailure("import", errors.New("row 3 refused")); err != nil {
		t.Fatalf("JournalMemoryFailure: %v", err)
	}
	events, err := graph.Events(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var failures []map[string]string
	for _, event := range events {
		if event.Kind != EventMemoryWriteFailed {
			continue
		}
		var payload map[string]string
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("decode %d: %v", event.Seq, err)
		}
		failures = append(failures, payload)
	}
	if len(failures) != 2 {
		t.Fatalf("failure events = %d, want 2", len(failures))
	}
	if failures[0]["via"] != "extract" || !strings.Contains(failures[0]["error"], "decider") {
		t.Fatalf("first failure = %+v, want via=extract and the reason", failures[0])
	}
	// A nil error is nothing to journal — the callers pass what they have.
	if err := graph.JournalMemoryFailure("extract", nil); err != nil {
		t.Fatalf("JournalMemoryFailure(nil) = %v, want a quiet no", err)
	}
}

// ── quarantine ──────────────────────────────────────────────────────────────

// A bare project scope — the legacy caller's spelling — lands in quarantine:
// visible to the person's audit reads, invisible to every injection.
func TestABareProjectScopeQuarantinesAndTheQuarantineNeverInjects(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "quarantine.db"))
	quarantined := mustAddMemory(t, graph, Memory{Type: MemoryDecision, Scope: MemoryScopeProject,
		Title: "Pricing stays annual", Text: "Pricing is billed annually, decided in the alpha project."})
	if quarantined.Owner != OwnerLegacyProject {
		t.Fatalf("a bare project write landed as %q, want %q", quarantined.Owner, OwnerLegacyProject)
	}
	if !containsStubTag(quarantined.Tags, OwnerLegacyTag) {
		t.Fatalf("quarantined row tags = %v, want the legacy marker", quarantined.Tags)
	}
	// A REAL view — the owners a session names — never contains the quarantine.
	realView := []string{OwnerUser, OwnerMachine, OwnerProject("alpha")}
	pool, err := graph.MemoryCandidates(realView, "pricing annual", MemoryCandidatesDefault)
	if err != nil {
		t.Fatal(err)
	}
	if containsMemoryStub(pool, quarantined.ID) {
		t.Errorf("a quarantined row reached the pool: %+v", pool)
	}
	hits, err := graph.SearchMemories([]string{OwnerUser}, "pricing", 5)
	if err != nil || len(hits) != 0 {
		t.Fatalf("a user search saw quarantine: (%v, %v)", memoryIDs(hits), err)
	}
	listed, err := graph.QuarantinedMemories(50)
	if err != nil || len(listed) != 1 || listed[0].ID != quarantined.ID {
		t.Fatalf("QuarantinedMemories = (%v, %v), want the row", memoryIDs(listed), err)
	}
}

// Re-homing moves a quarantined row to a provable owner, journaled, and can
// never touch a row that already has an owner.
func TestRehomeMovesOnlyQuarantinedRowsAndJournalsTheMove(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "rehome.db"))
	quarantined := mustAddMemory(t, graph, Memory{Type: MemoryDecision, Scope: MemoryScopeProject,
		Title: "Pricing stays annual", Text: "Pricing is billed annually."})
	held := mustAddMemory(t, graph, Memory{Owner: OwnerUser, Type: MemoryPreference,
		Title: "Wants terse replies", Text: "Answers short, conclusion first."})

	alpha := OwnerProject("alpha")
	moved, err := graph.RehomeMemories([]string{quarantined.ID, held.ID, "mem_absent"}, alpha)
	if err != nil || moved != 1 {
		t.Fatalf("RehomeMemories = (%d, %v), want exactly the one quarantined row", moved, err)
	}
	got, err := graph.GetMemories([]string{alpha}, []string{quarantined.ID})
	if err != nil || len(got) != 1 {
		t.Fatalf("the re-homed row under alpha = (%v, %v)", memoryIDs(got), err)
	}
	if !containsStubTag(got[0].Tags, OwnerLegacyTag) {
		t.Errorf("the re-homed row lost its marker: %v", got[0].Tags)
	}
	// The row with an owner was untouched.
	unchanged, err := graph.GetMemories([]string{OwnerUser}, []string{held.ID})
	if err != nil || len(unchanged) != 1 || unchanged[0].Owner != OwnerUser {
		t.Fatalf("a re-home touched an owned row: (%+v, %v)", unchanged, err)
	}
	// A re-home to an owner nobody mints is refused.
	if _, err := graph.RehomeMemories([]string{quarantined.ID}, "team:the-fold"); !errors.Is(err, ErrInvalid) {
		t.Errorf("RehomeMemories(team) = %v, want a refusal", err)
	}
	// The move is journaled.
	events, err := graph.Events(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	rehomed := 0
	for _, event := range events {
		if event.Kind == EventMemoryRehomed {
			rehomed++
		}
	}
	if rehomed != 1 {
		t.Fatalf("rehome events = %d, want 1", rehomed)
	}
}

// ── the migration ───────────────────────────────────────────────────────────

// A store written before owners existed comes out of the migration with every
// row owned the way its scope proves, and the unknown-project rows in
// quarantine — never re-attributed to the person at large.
func TestTheMigrationOwnsEveryRowWithoutGuessing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")
	graph := openTestStore(t, path)
	legacy := map[string]string{
		"user":    "A preference the person stated.",
		"env":     "A fact about this machine.",
		"project": "A decision from a project nobody can name.",
	}
	for scope, text := range legacy {
		mustAddMemory(t, graph, Memory{Type: MemoryFact, Scope: scope, Title: "Legacy row", Text: text})
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
	// Reopen: the migration runs on the open.
	graph, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer graph.Close()
	rows, err := graph.queryMemories(``, nil, `id`, 0)
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]string{}
	for _, row := range rows {
		owners[row.Text] = row.Owner
	}
	if got := owners[legacy["user"]]; got != OwnerUser {
		t.Errorf("the user row owns as %q, want %q", got, OwnerUser)
	}
	if got := owners[legacy["env"]]; got != OwnerMachine {
		t.Errorf("the env row owns as %q, want %q", got, OwnerMachine)
	}
	if got := owners[legacy["project"]]; got != OwnerLegacyProject {
		t.Errorf("the project row owns as %q, want %q — a guess is how a row widens", got, OwnerLegacyProject)
	}
	// AND THE MIGRATION IS IDEMPOTENT: a second open changes nothing.
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
	graph, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	rows2, err := graph.queryMemories(``, nil, `id`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rows, rows2) {
		t.Fatalf("a second migration changed rows:\nfirst  %+v\nsecond %+v", rows, rows2)
	}
}

// ── pagination ──────────────────────────────────────────────────────────────

// Pages walk the same ordering ListMemories uses, with a cursor that survives
// writes landing between pages, and end with More=false.
func TestMemoryPageWalksAStableCursor(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "pages.db"))
	const total = 25
	ids := make([]string, 0, total)
	for index := 0; index < total; index++ {
		memory := mustAddMemory(t, graph, Memory{Owner: OwnerUser, Type: MemoryFact,
			Title: fmt.Sprintf("Page row %02d", index),
			Text:  fmt.Sprintf("Page row number %02d, distinct words %d.", index, index*7)})
		ids = append(ids, memory.ID)
	}
	var seen []string
	cursor := MemoryCursor{}
	pages := 0
	for {
		page, next, err := graph.MemoryPage([]string{OwnerUser}, cursor, 10)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		pages++
		seen = append(seen, memoryIDs(page)...)
		if !next.More {
			break
		}
		cursor = next
		if pages > 10 {
			t.Fatal("the cursor never ended")
		}
	}
	if pages != 3 {
		t.Fatalf("pages = %d, want 3", pages)
	}
	// NEWEST TOUCHED FIRST is the list order, so the walk reads the ids the
	// test minted oldest-first backwards.
	want := make([]string, len(ids))
	for index, id := range ids {
		want[len(ids)-1-index] = id
	}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("walked %v, want newest-touched order %v", seen, want)
	}
	// A write landing between pages does not move the walk: the cursor is the
	// position, not an offset.
	first, next, err := graph.MemoryPage([]string{OwnerUser}, MemoryCursor{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	mustAddMemory(t, graph, Memory{Owner: OwnerUser, Type: MemoryFact, Title: "Landed between", Text: "A fresh row between two pages."})
	second, _, err := graph.MemoryPage([]string{OwnerUser}, next, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range append(append([]Memory{}, first...), second...) {
		if row.Title == "Landed between" {
			t.Fatalf("the interloper appeared on pages already walked: %+v", row)
		}
	}
	// Owners hold: another project's rows are on no page of this walk.
	other, err := graph.Write(WriteRequest{Owner: OwnerProject("elsewhere"), Type: MemoryFact, Title: "Elsewhere", Text: "Another project's truth."})
	if err != nil {
		t.Fatal(err)
	}
	page, _, err := graph.MemoryPage([]string{OwnerUser}, MemoryCursor{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if containsMemoryStub(toStubs(page), other.Memory.ID) {
		t.Errorf("another owner's row is on this page: %+v", page)
	}
}

func toStubs(memories []Memory) []MemoryStub {
	stubs := make([]MemoryStub, 0, len(memories))
	for _, memory := range memories {
		stubs = append(stubs, MemoryStub{ID: memory.ID, Title: memory.Title, Type: memory.Type, Scope: memory.Scope})
	}
	return stubs
}

func containsStubTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}

// ── regression: owner widening ───────────────────────────────────────────────

// An unknown owner in replay — a future team:… owner from a newer build — is
// quarantined, never widened to user. A row whose owner cannot be understood
// must never degrade to one visible from every project.
func TestOwnerForReplayQuarantinesAnUnknownOwner(t *testing.T) {
	// A payload with an explicit future team owner.
	payload := memoryPayload{ID: "mem_x", Owner: "team:the-fold", Scope: MemoryScopeUser}
	got := ownerForReplay(payload)
	if got != OwnerLegacyProject {
		t.Fatalf("ownerForReplay(team:the-fold) = %q, want %q (quarantine)", got, OwnerLegacyProject)
	}
	// An unknown scope word with no explicit owner also quarantines.
	payload2 := memoryPayload{ID: "mem_y", Scope: "global"}
	got2 := ownerForReplay(payload2)
	if got2 != OwnerLegacyProject {
		t.Fatalf("ownerForReplay(scope=global) = %q, want %q (quarantine)", got2, OwnerLegacyProject)
	}
}

// A memory event with an unknown owner — a newer build's team:… owner — is
// quarantined by the sync fold rather than widened.
func TestMemoryEventOwnerQuarantinesAnUnknownOwner(t *testing.T) {
	// A payload carrying an explicit owner this build does not mint.
	payload, _ := json.Marshal(memoryPayload{ID: "mem_x", Owner: "team:the-fold", Scope: MemoryScopeUser})
	event := Event{Kind: EventMemoryAdd, Payload: json.RawMessage(payload)}
	got := memoryEventOwner(event)
	if got != OwnerLegacyProject {
		t.Fatalf("memoryEventOwner(team:the-fold) = %q, want %q (quarantine)", got, OwnerLegacyProject)
	}
	// An unparseable payload also quarantines — a blob this build cannot decode
	// carries no owner it can assert.
	event2 := Event{Kind: EventMemoryAdd, Payload: json.RawMessage(`{bogus`)}
	got2 := memoryEventOwner(event2)
	if got2 != OwnerLegacyProject {
		t.Fatalf("memoryEventOwner(unparseable) = %q, want %q (quarantine)", got2, OwnerLegacyProject)
	}
}

// ── regression: owner-scoped write doors ─────────────────────────────────────

// A SupersedeMemoryForOwners that names a row the owners cannot see is refused.
func TestSupersedeForOwnersRefusesAForeignID(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "supersede-refuse.db"))
	alpha := OwnerProject("alpha")
	beta := OwnerProject("beta")

	alphaRow, err := graph.Write(WriteRequest{Owner: alpha, Type: MemoryFact,
		Title: "Deploys Tuesdays", Text: "Deploys to the amber cluster every Tuesday."})
	if err != nil {
		t.Fatal(err)
	}
	// Beta tries to supersede alpha's row through the scoped door.
	_, err = graph.SupersedeMemoryForOwners([]string{beta}, alphaRow.Memory.ID, Memory{
		Owner: beta, Type: MemoryFact,
		Title: "Deploys Wednesdays", Text: "Deploys to the ember cluster every Wednesday.",
	})
	if err == nil {
		t.Fatal("SupersedeMemoryForOwners accepted a foreign id — the door is open")
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("SupersedeMemoryForOwners = %v, want an ErrInvalid refusal", err)
	}
}

// An UpdateMemoryForOwners that names a row the owners cannot see is refused.
func TestUpdateForOwnersRefusesAForeignID(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "update-refuse.db"))
	alpha := OwnerProject("alpha")
	beta := OwnerProject("beta")

	alphaRow, err := graph.Write(WriteRequest{Owner: alpha, Type: MemoryFact,
		Title: "Amber cluster", Text: "The amber cluster is in us-east-1."})
	if err != nil {
		t.Fatal(err)
	}
	// Beta tries to update alpha's row through the scoped door.
	err = graph.UpdateMemoryForOwners([]string{beta}, alphaRow.Memory.ID,
		"Amber cluster", "The amber cluster is in us-west-2.", nil, "")
	if err == nil {
		t.Fatal("UpdateMemoryForOwners accepted a foreign id — the door is open")
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("UpdateMemoryForOwners = %v, want an ErrInvalid refusal", err)
	}
}

// ── regression: rehome fold validates the target owner ───────────────────────

// A rehomed event in the fold whose target owner is not valid is skipped —
// never applied to move a quarantined row to an owner this build does not mint.
func TestRehomedFoldRefusesAnInvalidTargetOwner(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "rehome-fold-refuse.db"))
	// Put a row in quarantine.
	quarantined := mustAddMemory(t, graph, Memory{Type: MemoryDecision, Scope: MemoryScopeProject,
		Title: "Pricing stays annual", Text: "Pricing is billed annually."})
	if quarantined.Owner != OwnerLegacyProject {
		t.Fatalf("quarantined row owner = %q, want %q", quarantined.Owner, OwnerLegacyProject)
	}
	// Construct a rehome event with a future team owner.
	payload, _ := json.Marshal(map[string]any{
		"ids":   []string{quarantined.ID},
		"owner": "team:the-fold",
	})
	events := []MemoryEvent{{Seq: 1, Kind: EventMemoryRehomed, Payload: json.RawMessage(payload)}}
	// Apply with a permissive policy — the fold itself should refuse the invalid
	// owner inside the apply.
	result, err := graph.ApplyMemoryEvents(events, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied != 0 {
		t.Fatalf("the rehome fold applied a future team owner: %d applied", result.Applied)
	}
	if result.Skipped == 0 {
		t.Fatal("the rehome fold did not skip the invalid-owner event")
	}
	// The row is still in quarantine.
	row, ok, err := graph.MemoryRecord(quarantined.ID)
	if err != nil || !ok || row.Owner != OwnerLegacyProject {
		t.Fatalf("after invalid-owner rehome fold: (%+v, %v, %v), want still in quarantine", row, ok, err)
	}
}

// ── regression: aged dedup ───────────────────────────────────────────────────

// The dedup door catches an exact duplicate that is older than the old 5-row
// FTS window. The store's Write door is the one path; prove it skips an exact
// repeat that is behind more than five active rows for the same owner.
func TestDedupCatchesAnExactDuplicateBeyondTheOldWindow(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "aged-dedup.db"))
	owner := OwnerUser
	// Write six fillers, then a target, then the duplicate — six rows after the
	// target means the old 5-neighbor window would miss it.
	for index := 0; index < 6; index++ {
		if _, err := graph.Write(WriteRequest{Owner: owner, Type: MemoryFact,
			Title: fmt.Sprintf("Filler %02d", index),
			Text:  fmt.Sprintf("Filler row number %02d with distinct words %x.", index, index)}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := graph.Write(WriteRequest{Owner: owner, Type: MemoryPreference,
		Title: "Prefers tabs", Text: "Prefers tabs over spaces in Go files."})
	if err != nil || first.Outcome != WriteOutcomeAdded {
		t.Fatalf("first write of the target: (%+v, %v)", first, err)
	}
	// Six more fillers push the target past the old 5-neighbor window.
	for index := 6; index < 12; index++ {
		if _, err := graph.Write(WriteRequest{Owner: owner, Type: MemoryFact,
			Title: fmt.Sprintf("Filler %02d", index),
			Text:  fmt.Sprintf("Filler row number %02d with distinct words %x.", index, index)}); err != nil {
			t.Fatal(err)
		}
	}
	// The exact repeat, different case and spacing.
	again, err := graph.Write(WriteRequest{Owner: owner, Type: MemoryPreference,
		Title: "Prefers tabs", Text: "PREFERS   tabs over spaces in Go files."})
	if err != nil || again.Outcome != WriteOutcomeSkipped {
		t.Fatalf("repeat past the old window = (%+v, %v), want a skip", again, err)
	}
	if again.Memory.ID != first.Memory.ID {
		t.Fatalf("the aged dedup matched %q, want the original %q", again.Memory.ID, first.Memory.ID)
	}
}
