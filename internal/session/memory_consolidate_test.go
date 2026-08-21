package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The tidy is tested the way a person would notice it: what is in the store
// afterwards, what it refused to touch, and — most often — that no call was
// made at all. A pass that fires when somebody is typing, or twice in an hour,
// or over a store nothing has happened to, is money spent on nothing while
// nobody is watching, which is the failure this file exists to hold shut.

// tidyScript answers the consolidation call and counts it. It knows the call by
// its system prompt, which is how the calls really differ on the wire.
type tidyScript struct {
	mu    sync.Mutex
	plan  string
	fail  error
	usd   float64
	calls int
}

func (s *tidyScript) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	system := ""
	if len(messages) > 0 {
		system = messageText(messages[0])
	}
	if !strings.Contains(system, "You tidy a person's remembered notes") {
		return nil, errors.New("the tidy asked something that was not the tidy")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.fail != nil {
		return nil, s.fail
	}
	response := textResponse(s.plan)
	if s.usd > 0 {
		cost := s.usd
		response.Usage.Cost = &cost
	}
	return response, nil
}

func (s *tidyScript) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// tidyBrain is a store of the test's own with the given rows in it, oldest
// first, and the pass that reads it.
func tidyBrain(t *testing.T, script Completer, rows ...store.Memory) (tidyPass, *store.Store, string) {
	t.Helper()
	root := t.TempDir()
	brain, err := store.Open(filepath.Join(root, "brain.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = brain.Close() })
	for _, row := range rows {
		if row.Type == "" {
			row.Type = store.MemoryFact
		}
		if row.Scope == "" {
			row.Scope = store.MemoryScopeUser
		}
		if _, err := brain.AddMemory(row); err != nil {
			t.Fatalf("add memory %q: %v", row.Title, err)
		}
	}
	return tidyPass{brain: brain, client: script, model: "tidy-model", root: root}, brain, root
}

func activeTitles(t *testing.T, brain *store.Store) []string {
	t.Helper()
	rows, err := brain.ListMemories("", 50)
	if err != nil {
		t.Fatalf("list memories: %v", err)
	}
	return titles(rows)
}

// ── the gates ───────────────────────────────────────────────────────────────

// MEMORY OFF IS THE SEAM ABSENT, not a pass that runs and declines. The tick
// asks whether it has anything to tidy with by whether the seam is there at
// all, exactly as it asks whether this build can judge or look.
func TestTheTidyIsAbsentWhenMemoryIsOff(t *testing.T) {
	always := standing.Idle(func(time.Duration) bool { return true })
	if tidy := NewMemoryTidy(Config{}, "", t.TempDir(), always); tidy != nil {
		t.Fatal("memory off still handed the tick something to call")
	}
	if tidy := NewMemoryTidy(Config{}, filepath.Join(t.TempDir(), "brain.db"), "", always); tidy != nil {
		t.Fatal("a tidy with nowhere to keep its watermark was still built")
	}
}

// NOBODY MAY BE HERE. The pass writes into the same store the very next message
// routes against, so a machine somebody is typing on is one it does not touch —
// and it must not even open the database to find that out.
func TestTheTidyWaitsUntilTheMachineIsQuiet(t *testing.T) {
	root := t.TempDir()
	var asked time.Duration
	busy := standing.Idle(func(quiet time.Duration) bool { asked = quiet; return false })

	tidy := NewMemoryTidy(Config{}, filepath.Join(root, "nothing-here.db"), root, busy)
	if tidy == nil {
		t.Fatal("the tidy was not built")
	}
	tidied, err := tidy(context.Background())
	if err != nil {
		t.Fatalf("a busy machine answered an error: %v", err)
	}
	if tidied.Changed() != 0 {
		t.Fatalf("a busy machine tidied %d lines", tidied.Changed())
	}
	if asked != consolidateQuiet {
		t.Fatalf("asked for %s of quiet, want %s", asked, consolidateQuiet)
	}
	if _, err := os.Stat(filepath.Join(root, "nothing-here.db")); err == nil {
		t.Fatal("a busy machine opened the store anyway")
	}
}

// ONCE EVERY SIX HOURS AND NO OFTENER. The tick comes round every five minutes;
// without the watermark this would be a call every five minutes, forever, over
// a list that changes a handful of times a day.
func TestTheTidyRunsAtMostOnceEveryFewHours(t *testing.T) {
	root := t.TempDir()
	always := standing.Idle(func(time.Duration) bool { return true })
	if err := writeConsolidateMark(root, consolidateMark{At: time.Now().Add(-time.Hour), Seq: 4}); err != nil {
		t.Fatalf("write watermark: %v", err)
	}
	tidy := NewMemoryTidy(Config{}, filepath.Join(root, "nothing-here.db"), root, always)
	if _, err := tidy(context.Background()); err != nil {
		t.Fatalf("a pass an hour after the last one answered an error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "nothing-here.db")); err == nil {
		t.Fatal("a pass inside the watermark opened the store anyway")
	}

	// And the same watermark, older than the floor, does let a pass through —
	// far enough to open the store, which is all this half is proving.
	if err := writeConsolidateMark(root, consolidateMark{At: time.Now().Add(-2 * consolidateEvery)}); err != nil {
		t.Fatalf("write watermark: %v", err)
	}
	// It gets as far as opening the store, which is all this half is proving:
	// with no model configured anywhere the call itself cannot be built, and
	// that is the honest answer rather than a pass that pretends to have run.
	_, _ = tidy(context.Background())
	if _, err := os.Stat(filepath.Join(root, "nothing-here.db")); err != nil {
		t.Fatalf("a pass past the watermark never opened the store: %v", err)
	}
}

// SOMETHING MUST HAVE MOVED. One new line has already been settled against its
// neighbours on the turn that wrote it, so a pass over a store with one change
// in it is a call paid for to be told what is already known.
func TestTheTidyNeedsTwoThingsToHaveChanged(t *testing.T) {
	script := &tidyScript{plan: `{"ops":[]}`}
	pass, brain, _ := tidyBrain(t, script,
		store.Memory{Title: "deploys on Fridays", Text: "they deploy on Fridays"},
		store.Memory{Title: "prefers tabs", Text: "they prefer tabs"},
	)
	rows, err := brain.ListMemories("", 50)
	if err != nil {
		t.Fatalf("list memories: %v", err)
	}
	// A watermark that has seen the older of the two leaves exactly one change.
	pass.mark = consolidateMark{At: time.Now().Add(-2 * consolidateEvery), Seq: rows[len(rows)-1].UpdatedSeq}
	if _, err := pass.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if calls := script.count(); calls != 0 {
		t.Fatalf("one change bought %d calls", calls)
	}

	// Two changes do buy one.
	pass.mark = consolidateMark{At: time.Now().Add(-2 * consolidateEvery)}
	if _, err := pass.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if calls := script.count(); calls != 1 {
		t.Fatalf("two changes bought %d calls, want 1", calls)
	}
}

// ── the plan ────────────────────────────────────────────────────────────────

// A REFINE IS A REWRITE IN PLACE, so every pointer at that id — the router's,
// the index's — stays valid, and its use count and its age survive.
func TestARefineRewritesOneLineInPlace(t *testing.T) {
	script := &tidyScript{}
	pass, brain, _ := tidyBrain(t, script,
		store.Memory{Title: "tabs", Text: "they prefer tabs"},
		store.Memory{Title: "tabs in go", Text: "they prefer tabs in go files"},
	)
	rows, _ := brain.ListMemories("", 50)
	target := rows[0]
	script.plan = mustPlan(t, consolidateOp{Op: "refine", ID: target.ID,
		Title: "prefers tabs", Text: "they prefer tabs, in go files and everywhere else"})

	tidied, err := pass.run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if tidied.Merged != 1 || tidied.Superseded != 0 {
		t.Fatalf("the pass came to %+v, want one merge", tidied)
	}
	after, found, err := brain.MemoryRecord(target.ID)
	if err != nil || !found {
		t.Fatalf("read back %q: %v", target.ID, err)
	}
	if after.Status != store.MemoryActive {
		t.Fatalf("a refined row is %q, want active", after.Status)
	}
	if after.Title != "prefers tabs" || !strings.Contains(after.Text, "everywhere else") {
		t.Fatalf("the row still says %q / %q", after.Title, after.Text)
	}
	if after.SourceSession != consolidateSource {
		t.Fatalf("the rewrite is journaled under %q, want %q", after.SourceSession, consolidateSource)
	}
}

// A SUPERSEDE RETIRES THE ROW AND PUTS ONE IN ITS PLACE, in one event, and the
// retired row stays readable — nothing here is ever deleted.
func TestASupersedeRetiresTheRowAndLeavesItReadable(t *testing.T) {
	script := &tidyScript{}
	pass, brain, _ := tidyBrain(t, script,
		store.Memory{Type: store.MemoryProjectState, Title: "on v2", Text: "the api is on v2"},
		store.Memory{Title: "deploys on Fridays", Text: "they deploy on Fridays"},
	)
	rows, _ := brain.ListMemories("", 50)
	var target store.Memory
	for _, row := range rows {
		if row.Type == store.MemoryProjectState {
			target = row
		}
	}
	script.plan = mustPlan(t, consolidateOp{Op: "supersede", ID: target.ID,
		Title: "on v3", Text: "the api is on v3"})

	tidied, err := pass.run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if tidied.Superseded != 1 || tidied.Merged != 0 {
		t.Fatalf("the pass came to %+v, want one supersede", tidied)
	}
	old, found, err := brain.MemoryRecord(target.ID)
	if err != nil || !found {
		t.Fatalf("the retired row is gone: %v", err)
	}
	if old.Status != store.MemorySuperseded {
		t.Fatalf("the retired row is %q, want superseded", old.Status)
	}
	names := activeTitles(t, brain)
	if !has(names, "on v3") || has(names, "on v2") {
		t.Fatalf("what is remembered is %v", names)
	}
	// The replacement keeps the retired row's type and scope: a replacement is
	// the same memory said better, not a different one in its place.
	for _, row := range mustList(t, brain) {
		if row.Title == "on v3" && (row.Type != store.MemoryProjectState || row.Scope != target.Scope) {
			t.Fatalf("the replacement is %q/%q, want %q/%q", row.Type, row.Scope, target.Type, target.Scope)
		}
	}
}

// THE ONE REFUSAL WRITTEN IN CODE. A preference, a decision and a correction
// are somebody's own words about how they want things; the pass may sharpen
// their wording and may never decide they have been replaced.
func TestTheTidyNeverRetiresSomebodysOwnWords(t *testing.T) {
	for _, kind := range []string{store.MemoryPreference, store.MemoryDecision, store.MemoryCorrection} {
		t.Run(kind, func(t *testing.T) {
			script := &tidyScript{}
			pass, brain, _ := tidyBrain(t, script,
				store.Memory{Type: kind, Title: "no force pushes", Text: "never force push to main"},
				store.Memory{Title: "deploys on Fridays", Text: "they deploy on Fridays"},
			)
			rows, _ := brain.ListMemories("", 50)
			var mine store.Memory
			for _, row := range rows {
				if row.Type == kind {
					mine = row
				}
			}
			script.plan = mustPlan(t, consolidateOp{Op: "supersede", ID: mine.ID,
				Title: "force pushes are fine", Text: "they are happy to force push"})

			tidied, err := pass.run(context.Background())
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if tidied.Changed() != 0 {
				t.Fatalf("a %s was retired by the tidy: %+v", kind, tidied)
			}
			after, _, err := brain.MemoryRecord(mine.ID)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if after.Status != store.MemoryActive || after.Text != "never force push to main" {
				t.Fatalf("the %s now reads %q (%s)", kind, after.Text, after.Status)
			}
			if has(activeTitles(t, brain), "force pushes are fine") {
				t.Fatal("the pass wrote the replacement it was refused")
			}

			// The same row may still be REFINED: rewriting words is something a
			// person can read and undo; retiring is what they would have to
			// notice to catch.
			script.plan = mustPlan(t, consolidateOp{Op: "refine", ID: mine.ID,
				Title: "never force push to main", Text: "never force push to main, on any project"})
			pass.mark = consolidateMark{}
			if tidied, err = pass.run(context.Background()); err != nil {
				t.Fatalf("run: %v", err)
			}
			if tidied.Merged != 1 {
				t.Fatalf("a %s could not be refined: %+v", kind, tidied)
			}
		})
	}
}

// AN OPERATION THIS PASS CANNOT MAKE SENSE OF CHANGES NOTHING AND STOPS
// NOTHING ELSE: a plan is a list of small decisions, and one bad id is not a
// reason to throw away the good ones beside it.
func TestABadOperationIsSkippedAndTheRestOfThePlanLands(t *testing.T) {
	script := &tidyScript{}
	pass, brain, _ := tidyBrain(t, script,
		store.Memory{Title: "one", Text: "the first thing"},
		store.Memory{Title: "two", Text: "the second thing"},
	)
	rows, _ := brain.ListMemories("", 50)
	script.plan = mustPlan(t,
		consolidateOp{Op: "refine", ID: "no-such-memory", Title: "ghost", Text: "nothing"},
		consolidateOp{Op: "sideways", ID: rows[0].ID, Title: "wrong", Text: "wrong"},
		consolidateOp{Op: "skip", ID: rows[1].ID},
		consolidateOp{Op: "refine", ID: rows[0].ID, Title: "one, clearly", Text: "the first thing, said once"},
		consolidateOp{Op: "refine", ID: rows[0].ID, Title: "one again", Text: "and again"},
	)
	tidied, err := pass.run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if tidied.Merged != 1 {
		t.Fatalf("the plan came to %+v, want exactly one merge", tidied)
	}
	names := activeTitles(t, brain)
	if !has(names, "one, clearly") || has(names, "ghost") || has(names, "one again") {
		t.Fatalf("what is remembered is %v", names)
	}
}

// EIGHT OPERATIONS IS THE BLAST RADIUS OF ONE BAD ANSWER. A pass that rewrote
// forty lines while nobody was watching is not a tidy.
func TestOnePassChangesAtMostEightLines(t *testing.T) {
	script := &tidyScript{}
	rows := make([]store.Memory, 0, consolidateOps+4)
	for i := 0; i < consolidateOps+4; i++ {
		rows = append(rows, store.Memory{
			Title: "line " + string(rune('a'+i)),
			Text:  "the thing at " + string(rune('a'+i)),
		})
	}
	pass, brain, _ := tidyBrain(t, script, rows...)
	stored, _ := brain.ListMemories("", 50)
	ops := make([]consolidateOp, 0, len(stored))
	for _, row := range stored {
		ops = append(ops, consolidateOp{Op: "refine", ID: row.ID, Title: row.Title + "!", Text: row.Text + "!"})
	}
	script.plan = mustPlan(t, ops...)

	tidied, err := pass.run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if tidied.Merged != consolidateOps {
		t.Fatalf("the pass changed %d lines, want %d", tidied.Merged, consolidateOps)
	}
}

// ── the watermark, the money and the line ───────────────────────────────────

// THE WATERMARK IS STAMPED AFTER THE WRITES, so the pass's own rewrites are not
// what the next pass reads as "something changed". Without this the same fifty
// lines would be tidied every six hours forever.
func TestAPassStampsTheClockPastItsOwnWrites(t *testing.T) {
	script := &tidyScript{usd: 0.0021}
	pass, brain, root := tidyBrain(t, script,
		store.Memory{Title: "one", Text: "the first thing"},
		store.Memory{Title: "two", Text: "the second thing"},
	)
	rows, _ := brain.ListMemories("", 50)
	script.plan = mustPlan(t, consolidateOp{Op: "refine", ID: rows[0].ID, Title: "one, clearly", Text: "said once"})

	tidied, err := pass.run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if tidied.USD != 0.0021 {
		t.Fatalf("the pass cost %v, want the call's own price", tidied.USD)
	}
	mark, found := readConsolidateMark(root)
	if !found {
		t.Fatal("no watermark was written")
	}
	if mark.Merged != 1 || mark.USD != 0.0021 {
		t.Fatalf("the watermark says %+v", mark)
	}
	after, _ := brain.ListMemories("", 1)
	if mark.Seq < after[0].UpdatedSeq {
		t.Fatalf("the watermark is at %d, behind the pass's own write at %d", mark.Seq, after[0].UpdatedSeq)
	}
	// And a second pass, with the clock wound past the floor, now sees nothing
	// changed and buys no call.
	before := script.count()
	pass.mark = consolidateMark{At: time.Now().Add(-2 * consolidateEvery), Seq: mark.Seq}
	if _, err := pass.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if script.count() != before {
		t.Fatal("the pass paid for a second call over its own writes")
	}
}

// NOTHING CHANGED IS NOTHING SAID, and a half that is zero is absent rather
// than printed as a zero. THE EMPTINESS LAW.
func TestTheTidyLineSaysOnlyWhatHappened(t *testing.T) {
	for _, want := range []struct {
		tidied standing.Tidied
		notice string
		ledger string
	}{
		{standing.Tidied{}, "", ""},
		{standing.Tidied{Merged: 2}, "memory tidied · 2 merged", "consolidated · 2 merged"},
		{standing.Tidied{Merged: 2, Superseded: 1}, "memory tidied · 2 merged · 1 superseded", "consolidated · 2 merged · 1 superseded"},
		{standing.Tidied{Superseded: 1, USD: 0.002}, "memory tidied · 1 superseded", "consolidated · 1 superseded · $0.002"},
	} {
		if got := consolidateNoticeLine(want.tidied); got != want.notice {
			t.Errorf("%+v says %q on screen, want %q", want.tidied, got, want.notice)
		}
		if got := want.tidied.Line(); got != want.ledger {
			t.Errorf("%+v says %q in the record, want %q", want.tidied, got, want.ledger)
		}
	}
}

// A CALL THAT FAILED IS A PASS THAT DID NOTHING, and the watermark is not
// stamped — the next idle hour tries again rather than waiting six for a
// provider that was down for one.
func TestAFailedCallLeavesTheWatermarkAlone(t *testing.T) {
	script := &tidyScript{fail: errors.New("the provider said no")}
	pass, _, root := tidyBrain(t, script,
		store.Memory{Title: "one", Text: "the first thing"},
		store.Memory{Title: "two", Text: "the second thing"},
	)
	if _, err := pass.run(context.Background()); err == nil {
		t.Fatal("a failed call answered no error")
	}
	if _, found := readConsolidateMark(root); found {
		t.Fatal("a failed pass stamped the clock")
	}
}

// ── the listing the call reads ──────────────────────────────────────────────

// TWO SCOPES ARE NEVER DUPLICATES OF EACH OTHER, however alike they read, so
// the listing groups by how far a line's truth reaches before anything is asked
// to merge anything.
func TestTheListingGroupsByHowFarTheTruthReaches(t *testing.T) {
	listing := consolidateListing([]store.Memory{
		{ID: "a", Type: store.MemoryFact, Scope: store.MemoryScopeUser, Title: "tabs", Text: "prefers tabs", UpdatedSeq: 2},
		{ID: "b", Type: store.MemoryProjectState, Scope: store.MemoryScopeProject, Title: "on v2", Text: "the api is on v2", UpdatedSeq: 1},
	})
	if !strings.Contains(listing, "true about them everywhere") ||
		!strings.Contains(listing, "true only inside one project") {
		t.Fatalf("the listing reads:\n%s", listing)
	}
	if !strings.Contains(listing, "a · fact · tabs — prefers tabs") {
		t.Fatalf("a row is not addressable in:\n%s", listing)
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func mustPlan(t *testing.T, ops ...consolidateOp) string {
	t.Helper()
	raw, err := json.Marshal(consolidatePlan{Ops: ops})
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	return string(raw)
}

func mustList(t *testing.T, brain *store.Store) []store.Memory {
	t.Helper()
	rows, err := brain.ListMemories("", 50)
	if err != nil {
		t.Fatalf("list memories: %v", err)
	}
	return rows
}

func has(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}
