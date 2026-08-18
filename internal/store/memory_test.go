package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// A memory that has been added must be reachable by every route the router
// has: by words, by scope, by the index it reads every turn, and by id.
func TestAddedMemoryIsReachableByEveryRoute(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "memories.db"))

	pricing := mustAddMemory(t, graph, Memory{
		Type: MemoryDecision, Scope: MemoryScopeProject,
		Title: "Pricing stays annual",
		Text:  "We decided pricing is billed annually with no monthly plan.",
		Tags:  []string{"pricing", "billing"},
	})
	editor := mustAddMemory(t, graph, Memory{
		Type: MemoryPreference, Scope: MemoryScopeUser,
		Title: "Prefers terse replies",
		Text:  "Wants short answers with the conclusion first.",
	})
	deploy := mustAddMemory(t, graph, Memory{
		Type: MemoryProjectState, Scope: MemoryScopeEnv,
		Title: "Staging runs on spark",
		Text:  "The staging deployment lives on the spark machine.",
		Tags:  []string{"deploy"},
	})

	if !strings.HasPrefix(pricing.ID, "mem_") {
		t.Errorf("minted id = %q, want a mem_ prefix", pricing.ID)
	}
	if pricing.Status != MemoryActive || pricing.CreatedSeq == 0 || pricing.UpdatedSeq != pricing.CreatedSeq {
		t.Errorf("added memory = %+v, want active with both sequences at creation", pricing)
	}

	hits, err := graph.SearchMemories("annual billing", 5)
	if err != nil {
		t.Fatalf("SearchMemories: %v", err)
	}
	if len(hits) == 0 || hits[0].ID != pricing.ID {
		t.Fatalf("search for the pricing decision ranked %v first, want %q", memoryIDs(hits), pricing.ID)
	}

	// A tag is a search term too: it is indexed beside the title and the text.
	tagged, err := graph.SearchMemories("deploy", 5)
	if err != nil {
		t.Fatalf("SearchMemories by tag: %v", err)
	}
	if len(tagged) != 1 || tagged[0].ID != deploy.ID {
		t.Fatalf("search by tag = %v, want just %q", memoryIDs(tagged), deploy.ID)
	}

	all, err := graph.ListMemories("", 0)
	if err != nil {
		t.Fatalf("ListMemories: %v", err)
	}
	if got := memoryIDs(all); !reflect.DeepEqual(got, []string{deploy.ID, editor.ID, pricing.ID}) {
		t.Fatalf("ListMemories = %v, want newest touched first", got)
	}
	scoped, err := graph.ListMemories(MemoryScopeUser, 0)
	if err != nil {
		t.Fatalf("ListMemories(user): %v", err)
	}
	if got := memoryIDs(scoped); !reflect.DeepEqual(got, []string{editor.ID}) {
		t.Fatalf("ListMemories(user) = %v, want only the user-scoped memory", got)
	}

	index, err := graph.MemoryIndex(0)
	if err != nil {
		t.Fatalf("MemoryIndex: %v", err)
	}
	want := []MemoryStub{
		{ID: deploy.ID, Title: deploy.Title, Type: MemoryProjectState, Scope: MemoryScopeEnv},
		{ID: editor.ID, Title: editor.Title, Type: MemoryPreference, Scope: MemoryScopeUser},
		{ID: pricing.ID, Title: pricing.Title, Type: MemoryDecision, Scope: MemoryScopeProject},
	}
	if !reflect.DeepEqual(index, want) {
		t.Fatalf("MemoryIndex = %+v, want %+v", index, want)
	}

	// The router asks in the order it decided on, and gets that order back.
	got, err := graph.GetMemories([]string{deploy.ID, pricing.ID, "mem_nothing"})
	if err != nil {
		t.Fatalf("GetMemories: %v", err)
	}
	if ids := memoryIDs(got); !reflect.DeepEqual(ids, []string{deploy.ID, pricing.ID}) {
		t.Fatalf("GetMemories = %v, want the asked order with the unknown id absent", ids)
	}
	if !reflect.DeepEqual(got[1].Tags, []string{"pricing", "billing"}) {
		t.Errorf("tags round-tripped as %v, want [pricing billing]", got[1].Tags)
	}
}

// An updated memory keeps its id and its place, and the words it used to have
// stop finding it — a correction the search still answers with is not a
// correction.
func TestUpdatedMemoryIsFoundByItsNewWordsOnly(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "update.db"))

	memory := mustAddMemory(t, graph, Memory{
		Type: MemoryProjectState, Scope: MemoryScopeProject,
		Title: "Benchmark harness",
		Text:  "The benchmark runs nightly on the octopus machine.",
		Tags:  []string{"bench"},
	})
	if err := graph.UpdateMemory(memory.ID, "Benchmark harness",
		"The benchmark runs nightly on the platypus machine.", []string{"bench", "nightly"}); err != nil {
		t.Fatalf("UpdateMemory: %v", err)
	}

	stale, err := graph.SearchMemories("octopus", 5)
	if err != nil {
		t.Fatalf("SearchMemories(stale): %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("the replaced word still finds %v, want nothing", memoryIDs(stale))
	}
	fresh, err := graph.SearchMemories("platypus", 5)
	if err != nil {
		t.Fatalf("SearchMemories(fresh): %v", err)
	}
	if len(fresh) != 1 || fresh[0].ID != memory.ID {
		t.Fatalf("the new word finds %v, want %q", memoryIDs(fresh), memory.ID)
	}

	read, err := graph.GetMemories([]string{memory.ID})
	if err != nil || len(read) != 1 {
		t.Fatalf("GetMemories after update = (%v, %v)", read, err)
	}
	if !strings.Contains(read[0].Text, "platypus") || !reflect.DeepEqual(read[0].Tags, []string{"bench", "nightly"}) {
		t.Fatalf("updated memory = %+v, want the new text and tags", read[0])
	}
	if read[0].UpdatedSeq <= read[0].CreatedSeq {
		t.Errorf("updated_seq %d did not move past created_seq %d", read[0].UpdatedSeq, read[0].CreatedSeq)
	}
	if err := graph.UpdateMemory("mem_nothing", "x", "y", nil); err == nil {
		t.Error("updating a memory that does not exist succeeded")
	}
}

// Supersession replaces what is believed and keeps what was believed: the old
// memory leaves every view in the same breath the new one enters them, and
// stays readable by id so the change can be accounted for later.
func TestSupersededMemoryLeavesTheViewsAndStaysReadableForAudit(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "supersede.db"))

	old := mustAddMemory(t, graph, Memory{
		Type: MemoryDecision, Scope: MemoryScopeProject,
		Title: "Deploy target",
		Text:  "Deployments go to the kestrel cluster.",
	})
	fresh, err := graph.SupersedeMemory(old.ID, Memory{
		Type: MemoryDecision, Scope: MemoryScopeProject,
		Title: "Deploy target",
		Text:  "Deployments go to the albatross cluster.",
	})
	if err != nil {
		t.Fatalf("SupersedeMemory: %v", err)
	}

	if hits, err := graph.SearchMemories("kestrel", 5); err != nil || len(hits) != 0 {
		t.Fatalf("search for the superseded text = (%v, %v), want nothing", memoryIDs(hits), err)
	}
	if hits, err := graph.SearchMemories("albatross", 5); err != nil || len(hits) != 1 || hits[0].ID != fresh.ID {
		t.Fatalf("search for the new text = (%v, %v), want %q", memoryIDs(hits), err, fresh.ID)
	}
	list, err := graph.ListMemories("", 0)
	if err != nil {
		t.Fatalf("ListMemories: %v", err)
	}
	if ids := memoryIDs(list); !reflect.DeepEqual(ids, []string{fresh.ID}) {
		t.Fatalf("ListMemories = %v, want only the replacement", ids)
	}
	index, err := graph.MemoryIndex(0)
	if err != nil || len(index) != 1 || index[0].ID != fresh.ID {
		t.Fatalf("MemoryIndex = (%+v, %v), want only the replacement", index, err)
	}
	if got, err := graph.GetMemories([]string{old.ID}); err != nil || len(got) != 0 {
		t.Fatalf("GetMemories(superseded) = (%v, %v), want nothing", memoryIDs(got), err)
	}

	record, ok, err := graph.MemoryRecord(old.ID)
	if err != nil || !ok {
		t.Fatalf("MemoryRecord(superseded) = (%v, %v)", ok, err)
	}
	if record.Status != MemorySuperseded || !strings.Contains(record.Text, "kestrel") {
		t.Fatalf("audit row = %+v, want the old text with status superseded", record)
	}
	if record.UpdatedSeq != fresh.CreatedSeq {
		t.Errorf("retirement seq %d and admission seq %d differ; supersession must be one event",
			record.UpdatedSeq, fresh.CreatedSeq)
	}
	if _, err := graph.SupersedeMemory(old.ID, Memory{
		Type: MemoryDecision, Scope: MemoryScopeProject, Title: "Again", Text: "Again.",
	}); err == nil {
		t.Error("superseding an already superseded memory succeeded")
	}
}

// Forgetting removes a memory from everywhere a reader can see and refuses to
// happen twice.
func TestForgottenMemoryIsGoneEverywhereAndRefusesASecondForget(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "forget.db"))

	memory := mustAddMemory(t, graph, Memory{
		Type: MemoryCorrection, Scope: MemoryScopeUser,
		Title: "Not a morning person",
		Text:  "Do not schedule anything before eleven.",
		Tags:  []string{"schedule"},
	})
	if err := graph.ForgetMemory(memory.ID); err != nil {
		t.Fatalf("ForgetMemory: %v", err)
	}

	if hits, err := graph.SearchMemories("schedule eleven", 5); err != nil || len(hits) != 0 {
		t.Fatalf("search after forgetting = (%v, %v), want nothing", memoryIDs(hits), err)
	}
	if list, err := graph.ListMemories("", 0); err != nil || len(list) != 0 {
		t.Fatalf("ListMemories after forgetting = (%v, %v), want nothing", memoryIDs(list), err)
	}
	if index, err := graph.MemoryIndex(0); err != nil || len(index) != 0 {
		t.Fatalf("MemoryIndex after forgetting = (%+v, %v), want nothing", index, err)
	}
	if got, err := graph.GetMemories([]string{memory.ID}); err != nil || len(got) != 0 {
		t.Fatalf("GetMemories after forgetting = (%v, %v), want nothing", memoryIDs(got), err)
	}
	record, ok, err := graph.MemoryRecord(memory.ID)
	if err != nil || !ok || record.Status != MemoryForgotten {
		t.Fatalf("MemoryRecord(forgotten) = (%+v, %v, %v), want the row kept for audit", record, ok, err)
	}

	if err := graph.ForgetMemory(memory.ID); err == nil {
		t.Error("forgetting the same memory twice succeeded; the second must say nothing happened")
	}
	if err := graph.ForgetMemory("mem_nothing"); err == nil {
		t.Error("forgetting a memory that does not exist succeeded")
	}
	// A forgotten memory may not be corrected back into existence.
	if err := graph.UpdateMemory(memory.ID, "Not a morning person", "Anything.", nil); err == nil {
		t.Error("updating a forgotten memory succeeded")
	}
}

// The events table is truth: wiping both views and replaying the journal must
// put back exactly the same memories and exactly the same index.
func TestRebuildReproducesMemoriesAndTheirIndex(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "rebuild.db"))

	kept := mustAddMemory(t, graph, Memory{
		Type: MemoryFact, Scope: MemoryScopeProject,
		Title: "Store is event sourced",
		Text:  "The events table is truth and every other table is a view.",
		Tags:  []string{"store"},
	})
	corrected := mustAddMemory(t, graph, Memory{
		Type: MemoryPreference, Scope: MemoryScopeUser,
		Title: "Editor of choice",
		Text:  "Uses helix, not neovim.",
	})
	replaced := mustAddMemory(t, graph, Memory{
		Type: MemoryDecision, Scope: MemoryScopeProject,
		Title: "Release cadence",
		Text:  "We ship on the first Tuesday of the month.",
	})
	dropped := mustAddMemory(t, graph, Memory{
		Type: MemoryProjectState, Scope: MemoryScopeEnv,
		Title: "Scratch cluster",
		Text:  "A scratch cluster exists at hangar seven.",
	})
	if err := graph.UpdateMemory(corrected.ID, "Editor of choice", "Uses helix exclusively.", []string{"tools"}); err != nil {
		t.Fatalf("UpdateMemory: %v", err)
	}
	fresh, err := graph.SupersedeMemory(replaced.ID, Memory{
		Type: MemoryDecision, Scope: MemoryScopeProject,
		Title: "Release cadence", Text: "We ship continuously.",
	})
	if err != nil {
		t.Fatalf("SupersedeMemory: %v", err)
	}
	if err := graph.ForgetMemory(dropped.ID); err != nil {
		t.Fatalf("ForgetMemory: %v", err)
	}

	before := memoryRows(t, graph)
	indexBefore := memoryFTSRows(t, graph)
	if len(indexBefore) != 3 {
		t.Fatalf("index rows before rebuild = %d, want 3 active memories", len(indexBefore))
	}

	if _, err := graph.db.Exec(`DELETE FROM memories_fts`); err != nil {
		t.Fatalf("wipe memory index: %v", err)
	}
	if _, err := graph.db.Exec(`DELETE FROM memories`); err != nil {
		t.Fatalf("wipe memories: %v", err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	if after := memoryRows(t, graph); !reflect.DeepEqual(after, before) {
		t.Fatalf("memories after rebuild = %+v, want %+v", after, before)
	}
	if after := memoryFTSRows(t, graph); !reflect.DeepEqual(after, indexBefore) {
		t.Fatalf("memory index after rebuild = %+v, want %+v", after, indexBefore)
	}
	if hits, err := graph.SearchMemories("continuously", 5); err != nil || len(hits) != 1 || hits[0].ID != fresh.ID {
		t.Fatalf("search after rebuild = (%v, %v), want %q", memoryIDs(hits), err, fresh.ID)
	}
	if hits, err := graph.SearchMemories("hangar", 5); err != nil || len(hits) != 0 {
		t.Fatalf("the forgotten memory came back into search: (%v, %v)", memoryIDs(hits), err)
	}
	if got, err := graph.GetMemories([]string{kept.ID}); err != nil || len(got) != 1 {
		t.Fatalf("GetMemories after rebuild = (%v, %v)", memoryIDs(got), err)
	}
}

// Use counts are telemetry, not journaled truth: they move on retrieval and
// Rebuild deliberately resets them, so nothing downstream may rank on them.
func TestMemoryUseCountsAreTelemetryRebuildResets(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "uses.db"))

	first := mustAddMemory(t, graph, Memory{
		Type: MemoryFact, Scope: MemoryScopeUser,
		Title: "Timezone", Text: "Works from Pacific time.",
	})
	second := mustAddMemory(t, graph, Memory{
		Type: MemoryFact, Scope: MemoryScopeUser,
		Title: "Handle", Text: "Goes by santosh everywhere.",
	})
	if err := graph.BumpMemoryUse([]string{first.ID, first.ID, second.ID, "mem_nothing", ""}); err != nil {
		t.Fatalf("BumpMemoryUse: %v", err)
	}
	got, err := graph.GetMemories([]string{first.ID, second.ID})
	if err != nil || len(got) != 2 {
		t.Fatalf("GetMemories = (%v, %v)", memoryIDs(got), err)
	}
	if got[0].UseCount != 2 || got[1].UseCount != 1 {
		t.Fatalf("use counts = (%d, %d), want (2, 1)", got[0].UseCount, got[1].UseCount)
	}

	if err := graph.Rebuild(); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	rebuilt, err := graph.GetMemories([]string{first.ID, second.ID})
	if err != nil || len(rebuilt) != 2 {
		t.Fatalf("GetMemories after rebuild = (%v, %v)", memoryIDs(rebuilt), err)
	}
	if rebuilt[0].UseCount != 0 || rebuilt[1].UseCount != 0 {
		t.Fatalf("use counts after rebuild = (%d, %d), want both reset to 0",
			rebuilt[0].UseCount, rebuilt[1].UseCount)
	}
}

// What cannot be stored is refused at the door, by name, and nothing is
// journaled for it.
func TestMemoryCapsAndEnumsRefuseWhatCannotBeStored(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "caps.db"))

	sound := Memory{Type: MemoryFact, Scope: MemoryScopeUser, Title: "Fine", Text: "Fine."}
	for _, refusal := range []struct {
		name   string
		memory Memory
	}{
		{"unknown type", Memory{Type: "vibe", Scope: MemoryScopeUser, Title: "T", Text: "Body."}},
		{"empty type", Memory{Scope: MemoryScopeUser, Title: "T", Text: "Body."}},
		{"unknown scope", Memory{Type: MemoryFact, Scope: MemoryScopeUser + "s", Title: "T", Text: "Body."}},
		{"empty scope", Memory{Type: MemoryFact, Title: "T", Text: "Body."}},
		{"empty text", Memory{Type: MemoryFact, Scope: MemoryScopeUser, Title: "T", Text: "   "}},
		{"title over the cap", withTitle(sound, strings.Repeat("t", MemoryTitleRunes+1))},
		{"text over the cap", withText(sound, strings.Repeat("x", MemoryTextRunes+1))},
		{"too many tags", withTags(sound, MemoryMaxTags+1)},
	} {
		if _, err := graph.AddMemory(refusal.memory); err == nil {
			t.Errorf("AddMemory accepted %s", refusal.name)
		} else if !errors.Is(err, ErrInvalid) {
			t.Errorf("AddMemory(%s) = %v, want an ErrInvalid refusal", refusal.name, err)
		}
	}

	// Runes, not bytes: a cap measured in bytes would refuse a shorter sentence
	// for being written in a different alphabet.
	if _, err := graph.AddMemory(withTitle(sound, strings.Repeat("的", MemoryTitleRunes))); err != nil {
		t.Errorf("AddMemory refused a title of exactly %d runes: %v", MemoryTitleRunes, err)
	}
	if _, err := graph.AddMemory(withText(sound, strings.Repeat("的", MemoryTextRunes))); err != nil {
		t.Errorf("AddMemory refused a text of exactly %d runes: %v", MemoryTextRunes, err)
	}
	if err := graph.UpdateMemory("mem_x", strings.Repeat("t", MemoryTitleRunes+1), "Body.", nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("UpdateMemory past the title cap = %v, want an ErrInvalid refusal", err)
	}

	// A refusal writes nothing: the only memories in the store are the two the
	// rune checks above deliberately accepted.
	list, err := graph.ListMemories("", 0)
	if err != nil {
		t.Fatalf("ListMemories: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("stored memories = %d, want only the 2 accepted ones", len(list))
	}
}

// Two handles on the same brain file, adding at once: every memory lands
// exactly once, with its own id and its own event.
func TestConcurrentMemoryAddsAllLandExactlyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared-memories.db")
	first := openTestStore(t, path)
	second, err := Open(path)
	if err != nil {
		t.Fatalf("Open second handle: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	const writers = 8
	const each = 6
	start := make(chan struct{})
	added := make(chan Memory, writers*each)
	failures := make(chan error, writers*each)
	var wait sync.WaitGroup
	for writer := 0; writer < writers; writer++ {
		wait.Add(1)
		go func(writer int) {
			defer wait.Done()
			<-start
			handle := first
			if writer%2 == 1 {
				handle = second
			}
			for index := 0; index < each; index++ {
				memory, err := handle.AddMemory(Memory{
					Type: MemoryFact, Scope: MemoryScopeProject,
					Title: fmt.Sprintf("Writer %d note %d", writer, index),
					Text:  fmt.Sprintf("Writer %02d wrote note %02d during contention.", writer, index),
					Tags:  []string{fmt.Sprintf("writer%02d", writer)},
				})
				if err != nil {
					failures <- err
					continue
				}
				added <- memory
			}
		}(writer)
	}
	close(start)
	wait.Wait()
	close(added)
	close(failures)
	for err := range failures {
		t.Errorf("AddMemory returned an error under contention: %v", err)
	}

	ids := make(map[string]bool, writers*each)
	seqs := make(map[int64]bool, writers*each)
	for memory := range added {
		if ids[memory.ID] {
			t.Errorf("memory id %q was minted twice", memory.ID)
		}
		if seqs[memory.CreatedSeq] {
			t.Errorf("two memories claim event %d", memory.CreatedSeq)
		}
		ids[memory.ID] = true
		seqs[memory.CreatedSeq] = true
	}
	if len(ids) != writers*each {
		t.Fatalf("memories added = %d, want %d", len(ids), writers*each)
	}
	index, err := second.MemoryIndex(0)
	if err != nil {
		t.Fatalf("MemoryIndex: %v", err)
	}
	if len(index) != writers*each {
		t.Fatalf("index rows = %d, want %d", len(index), writers*each)
	}
	for _, stub := range index {
		if !ids[stub.ID] {
			t.Errorf("index carries %q, which no writer reports adding", stub.ID)
		}
	}
	// The views survive replay of everything that just landed concurrently.
	if err := first.Rebuild(); err != nil {
		t.Fatalf("Rebuild after contention: %v", err)
	}
	if rebuilt, err := first.MemoryIndex(0); err != nil || len(rebuilt) != writers*each {
		t.Fatalf("index after rebuild = (%d rows, %v), want %d", len(rebuilt), err, writers*each)
	}
}

func mustAddMemory(t *testing.T, graph *Store, m Memory) Memory {
	t.Helper()
	stored, err := graph.AddMemory(m)
	if err != nil {
		t.Fatalf("AddMemory(%q): %v", m.Title, err)
	}
	return stored
}

func memoryIDs(memories []Memory) []string {
	ids := make([]string, len(memories))
	for index, memory := range memories {
		ids[index] = memory.ID
	}
	return ids
}

func withTitle(m Memory, title string) Memory { m.Title = title; return m }
func withText(m Memory, text string) Memory   { m.Text = text; return m }

func withTags(m Memory, count int) Memory {
	m.Tags = make([]string, count)
	for index := range m.Tags {
		m.Tags[index] = fmt.Sprintf("tag%02d", index)
	}
	return m
}

// memoryRows reads every memory whatever its status, which is what a rebuild
// has to reproduce — the active view alone would pass while the audit trail
// silently vanished.
func memoryRows(t *testing.T, graph *Store) []Memory {
	t.Helper()
	rows, err := graph.queryMemories(``, nil, `id`, 0)
	if err != nil {
		t.Fatalf("read memory rows: %v", err)
	}
	return rows
}

type memoryIndexRow struct{ ID, Title, Text, Tags string }

func memoryFTSRows(t *testing.T, graph *Store) []memoryIndexRow {
	t.Helper()
	rows, err := graph.db.Query(`
		SELECT memory_id, title, text, tags FROM memories_fts ORDER BY memory_id`)
	if err != nil {
		t.Fatalf("read memory index: %v", err)
	}
	defer rows.Close()
	indexed := make([]memoryIndexRow, 0, 8)
	for rows.Next() {
		var row memoryIndexRow
		if err := rows.Scan(&row.ID, &row.Title, &row.Text, &row.Tags); err != nil {
			t.Fatalf("scan memory index: %v", err)
		}
		indexed = append(indexed, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read memory index: %v", err)
	}
	return indexed
}
