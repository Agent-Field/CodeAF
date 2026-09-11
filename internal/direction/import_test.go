package direction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

var importRun = ImportRun{ID: "run-1", Mode: "apply", Binary: "test"}

func legacyHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// holdItem is one version of an accepted hold as the old store has it: its
// content hash changes with its version and wording, and its receipt is the
// delegated one the old store recorded.
func holdItem(id, version, text string) ImportItem {
	return ImportItem{
		Legacy: Legacy{Store: LegacyStanding, ID: id, Version: version, SHA256: legacyHash(id + "\x00" + version + "\x00" + text)},
		Revisions: []ImportRevision{{Draft: rule(text, chatTarget("w")), State: Accepted,
			Receipt: Receipt{Actor: ActorLegacyDelegated, Door: DoorCard, Ref: "proposal-" + version}}},
	}
}

// contextItem is a shared-context record with its whole history, each revision
// stamped with the time the old store wrote it.
func contextItem(id string, texts ...string) ImportItem {
	revs := make([]ImportRevision, len(texts))
	for i, text := range texts {
		revs[i] = ImportRevision{Draft: finding(text, chatTarget("w")), State: Informational,
			WrittenAt: time.Date(2026, 1, 1+i, 0, 0, 0, 0, time.UTC)}
	}
	return ImportItem{
		Legacy: Legacy{Store: LegacyContexts, ID: id, Version: fmt.Sprint(len(texts)), SHA256: legacyHash(strings.Join(texts, "\x00"))},
		ID:     id, Revisions: revs,
	}
}

func countRecords(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.ws.ReadSnapshot(context.Background(), func(tx *sqlTx) error {
		return tx.QueryRowContext(context.Background(), "SELECT count(*) FROM direction_records").Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

// AN IMPORT NEVER SPELLS THE PERSON (C24, design §4.1 and F9). What an old store
// recorded is copied as legacy evidence; a person receipt is made only by a
// PersonReceipt constructor, so an import item that claims one is refused
// whole, through every door it might name.
func TestAnImportCannotCarryAPersonReceipt(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	for _, door := range []Door{DoorCard, DoorTerminal, DoorPage, DoorMigration} {
		item := holdItem("hold-"+string(door), "1", "reports never include phone numbers")
		item.Revisions[0].Receipt = Receipt{Actor: ActorPerson, Door: door, Ref: "proposal-1"}
		if res, err := s.Import(ctx, importRun, item); err == nil {
			t.Errorf("an import through the %s door wrote a person receipt: %+v", door, res)
		}
	}
	if n := countRecords(t, s); n != 0 {
		t.Fatalf("refused imports left %d records", n)
	}
	// A hold the person adopted through the old store's own door is legacy
	// evidence of that: it governs, labelled, and is never the person.
	item := holdItem("hold-adopted", "1", "reports never include phone numbers")
	item.Revisions[0].Receipt = Receipt{Actor: ActorLegacyPerson, Door: DoorCard, Ref: "proposal-1"}
	res, err := s.Import(ctx, importRun, item)
	if err != nil {
		t.Fatal(err)
	}
	if got := musts(t)(s.Current(ctx, res.Record)); got.Receipt.Actor != ActorLegacyPerson || got.Author.Class != AuthorMigration || got.Lane() != LaneGoverning {
		t.Fatalf("the adopted hold: %+v", got)
	}
}

// A STALE IMPORTER NEVER APPENDS OVER A NEWER ONE (L1). Importer A read the
// hold at version 2; importer B imported version 3 first. When A writes, its
// version is older than what the record already carries, so nothing is written.
func TestAStaleImportNeverAppendsOverANewerOne(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	must := musts(t)
	first, err := s.Import(ctx, importRun, holdItem("hold-1", "1", "at most $200 a night"))
	if err != nil {
		t.Fatal(err)
	}
	newer, err := s.Import(ctx, importRun, holdItem("hold-1", "3", "at most $300 a night"))
	if err != nil || newer.Outcome != Appended {
		t.Fatalf("importer B: %+v, %v", newer, err)
	}
	stale, err := s.Import(ctx, importRun, holdItem("hold-1", "2", "at most $250 a night"))
	cur := must(s.Current(ctx, first.Record))
	if err != nil || stale.Outcome == Appended || cur.Revision != newer.Revision || cur.Text != "at most $300 a night" {
		t.Fatalf("importer A's stale version: %+v, %v; the record is now revision %d %q", stale, err, cur.Revision, cur.Text)
	}
	if stale.Outcome != Stale || !strings.Contains(stale.Reason, "not newer") {
		t.Fatalf("a stale version is a no-op with a report line: %+v", stale)
	}
	// The same version with other content is no newer either.
	same, err := s.Import(ctx, importRun, holdItem("hold-1", "3", "at most $350 a night"))
	if err != nil || same.Outcome != Stale || musts(t)(s.Current(ctx, first.Record)).Revision != newer.Revision {
		t.Fatalf("an equal version: %+v, %v", same, err)
	}
}

// AN IMPORT NEVER OVERWRITES WHAT A PERSON CHANGED. The person withdrew an
// imported hold; the old store's hold then changed. Re-importing it would put
// the legacy accepted state back over the person's withdrawal, so it is
// refused and the withdrawal stands.
func TestAnImportNeverOverwritesWhatThePersonChanged(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	must := musts(t)
	first, err := s.Import(ctx, importRun, holdItem("hold-1", "1", "at most $200 a night"))
	if err != nil {
		t.Fatal(err)
	}
	withdrawn := must(s.Withdraw(ctx, Fence{ID: first.Record, Revision: first.Revision}, "paused", card(t, "pause it")))
	res, err := s.Import(ctx, importRun, holdItem("hold-1", "2", "at most $250 a night"))
	cur := must(s.Current(ctx, first.Record))
	if err == nil || cur.Revision != withdrawn.Revision || cur.State != Withdrawn {
		t.Fatalf("an import over the person's withdrawal: %+v, %v; the record is now revision %d %s", res, err, cur.Revision, cur.State)
	}
	if !errors.Is(err, ErrConflict) || res.Outcome != Refused || !strings.Contains(res.Reason, "that change stands") {
		t.Fatalf("the refusal is ErrConflict with a report line: %+v, %v", res, err)
	}
}

// A SHARED-CONTEXT RECORD KEEPS EVERY (ID, REVISION) (design §3.4, F10). It
// was imported at revision 1 and again once it had three; revisions 2 and 3
// land under their own numbers, so a receipt citing (C, 2) reads what the old
// store's revision 2 said.
func TestAContextImportedInStepsKeepsEveryRevisionNumber(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	must := musts(t)
	id := strings.Repeat("c", 32)
	texts := []string{"the venue holds 40", "the venue holds 45", "the venue holds 50"}
	if _, err := s.Import(ctx, importRun, contextItem(id, texts[:1]...)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Import(ctx, importRun, contextItem(id, texts...)); err != nil {
		t.Fatal(err)
	}
	for i, text := range texts {
		r, err := s.At(ctx, id, i+1)
		if err != nil || r.Text != text {
			t.Fatalf("(%s, %d) reads %q, %v; the old store's revision %d said %q", id[:6], i+1, r.Text, err, i+1, text)
		}
	}
	if cur := must(s.Current(ctx, id)); cur.Revision != len(texts) {
		t.Fatalf("the record stands at revision %d, want %d", cur.Revision, len(texts))
	}
}

// A HISTORY THAT DISAGREES IS REFUSED, NEVER OVERWRITTEN. The old store now
// says revision 2 was something else; the stored revision 2 is what receipts
// cite, so the import stops and reports rather than rewriting or appending.
func TestADivergentContextHistoryIsRefusedNotOverwritten(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	must := musts(t)
	id := strings.Repeat("d", 32)
	if _, err := s.Import(ctx, importRun, contextItem(id, "parking is on level 1", "parking is on level 2")); err != nil {
		t.Fatal(err)
	}
	res, err := s.Import(ctx, importRun, contextItem(id, "parking is on level 1", "parking is on level 3", "parking is free"))
	cur := must(s.Current(ctx, id))
	two := must(s.At(ctx, id, 2))
	if err == nil || cur.Revision != 2 || two.Text != "parking is on level 2" {
		t.Fatalf("a divergent history: %+v, %v; the record is at revision %d and (C, 2) reads %q", res, err, cur.Revision, two.Text)
	}
	if !errors.Is(err, ErrConflict) || res.Outcome != Refused || !strings.Contains(res.Reason, "revision 2") {
		t.Fatalf("the refusal is ErrConflict naming the first revision that differs: %+v, %v", res, err)
	}
}

// A FINISHED RUN RECORDS A BOUNDED ACCOUNT OF ITSELF: a JSON object of at most
// maxCounts bytes, kept forever with the report it names.
func TestAFinishedImportRunKeepsABoundedCount(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	if _, err := s.Import(ctx, importRun, holdItem("hold-1", "1", "formal tone")); err != nil {
		t.Fatal(err)
	}
	for name, counts := range map[string]string{
		"not JSON":   "imported=1",
		"not object": "[1,2]",
		"too long":   `{"note":"` + strings.Repeat("x", maxCounts) + `"}`,
	} {
		if err := s.FinishImportRun(ctx, importRun.ID, "", counts); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := s.FinishImportRun(ctx, importRun.ID, "", `{"imported":1}`); err != nil {
		t.Fatal(err)
	}
}
