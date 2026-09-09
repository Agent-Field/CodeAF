package workspace

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func createTestContext(t *testing.T, s *Store, title, text string, source Ref, targets ...Ref) ContextRecord {
	t.Helper()
	record, err := s.CreateContext(context.Background(), title, text, source, targets)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func contextIDs(records []ContextRecord) []string {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	return ids
}

// ONE RECORD, SEVERAL PLACES, ONE IDENTITY. The finding that a coding
// investigation contradicts a marketing claim is reachable from both efforts
// without becoming two transcripts that can disagree, and a revision made once
// is the revision both of them read.
func TestOneRecordAppliesInTwoPlacesAndRevisesInBoth(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "context.db")
	s := openTestStore(t, path)
	marketing := createTestCollection(t, s, "Marketing")
	coding := Ref{Kind: ConversationKind, ID: "auth-investigation"}
	source := Ref{Kind: TaskKind, ID: "4", SessionID: "auth-investigation"}
	record := createTestContext(t, s, "Setup takes eleven minutes",
		"Measured on a clean machine.\nThe claimed two minutes excludes the migration step.",
		source, Ref{Kind: CollectionKind, ID: marketing.ID}, coding)
	if record.Revision != 1 || record.Withdrawn {
		t.Fatalf("a new record starts current at revision 1: %+v", record)
	}
	for _, target := range record.Targets {
		found, err := s.ContextFor(ctx, []Ref{target})
		if err != nil || !reflect.DeepEqual(contextIDs(found), []string{record.ID}) {
			t.Fatalf("%v does not reach the record: %v, %v", target, contextIDs(found), err)
		}
	}
	// Asking about both places at once is a union, not a duplicate.
	both, err := s.ContextFor(ctx, record.Targets)
	if err != nil || len(both) != 1 || both[0].ID != record.ID {
		t.Fatalf("the union duplicated one record: %v, %v", contextIDs(both), err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTestStore(t, path)
	corrected := Ref{Kind: ArtifactKind, ID: filepath.Join(t.TempDir(), "timings.md")}
	revised, err := s.ReviseContext(ctx, record.ID, 1, "Setup takes nine minutes",
		"Re-measured with the migration cached.", corrected, record.Targets)
	if err != nil || revised.Revision != 2 {
		t.Fatalf("revise: %+v, %v", revised, err)
	}
	for _, target := range record.Targets {
		found, err := s.ContextFor(ctx, []Ref{target})
		if err != nil || len(found) != 1 {
			t.Fatalf("%v: %v, %v", target, contextIDs(found), err)
		}
		if found[0].Revision != 2 || found[0].Title != "Setup takes nine minutes" || found[0].Source != corrected {
			t.Fatalf("%v still reads an old revision: %+v", target, found[0])
		}
	}
}

// APPLICABILITY IS REPLACED WHOLE, so a revision that drops a place stops being
// current there, while the record itself keeps its identity and its history.
func TestARevisionThatDropsAPlaceStopsApplyingThere(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "targets.db"))
	product, marketing := createTestCollection(t, s, "Product"), createTestCollection(t, s, "Marketing")
	productRef := Ref{Kind: CollectionKind, ID: product.ID}
	marketingRef := Ref{Kind: CollectionKind, ID: marketing.ID}
	source := Ref{Kind: ConversationKind, ID: "chat"}
	record := createTestContext(t, s, "Pricing decision", "Annual only for now.", source, productRef, marketingRef)
	if _, err := s.ReviseContext(ctx, record.ID, 1, "Pricing decision", "Annual only for now.", source, []Ref{productRef}); err != nil {
		t.Fatal(err)
	}
	found, err := s.ContextFor(ctx, []Ref{marketingRef})
	if err != nil || len(found) != 0 {
		t.Fatalf("a dropped place still reads the record: %v, %v", contextIDs(found), err)
	}
	found, err = s.ContextFor(ctx, []Ref{productRef})
	if err != nil || len(found) != 1 {
		t.Fatalf("the kept place lost the record: %v, %v", contextIDs(found), err)
	}
	history, err := s.ContextHistory(ctx, record.ID)
	if err != nil || len(history) != 2 || len(history[0].Targets) != 2 || len(history[1].Targets) != 1 {
		t.Fatalf("the old applicability is not in the history: %+v, %v", history, err)
	}
}

// Membership, nesting and similarity are not applicability. A record filed
// against a folder does not descend into the folder's chats, and a record about
// a child collection does not climb to its parent. THE ONLY REACH IS THE
// EXPLICIT TARGET LIST, because organization is not relevance and neither is
// authority.
func TestContextForReachesOnlyItsOwnExplicitTargets(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "direct.db"))
	startup, product := createTestCollection(t, s, "Startup"), createTestCollection(t, s, "Product")
	if err := s.Add(ctx, startup.ID, Ref{Kind: CollectionKind, ID: product.ID}); err != nil {
		t.Fatal(err)
	}
	chat := Ref{Kind: ConversationKind, ID: "product-chat"}
	if err := s.Add(ctx, product.ID, chat); err != nil {
		t.Fatal(err)
	}
	source := Ref{Kind: ConversationKind, ID: "origin"}
	createTestContext(t, s, "Applies to Product only", "A note filed against one folder.",
		source, Ref{Kind: CollectionKind, ID: product.ID})
	for _, target := range []Ref{{Kind: CollectionKind, ID: startup.ID}, chat} {
		found, err := s.ContextFor(ctx, []Ref{target})
		if err != nil || len(found) != 0 {
			t.Fatalf("%v inherited context it was never given: %v, %v", target, contextIDs(found), err)
		}
	}
	// Task numbers repeat in every conversation, so a target is only reached by
	// the whole address, not by the number that happens to match.
	mine := Ref{Kind: TaskKind, ID: "1", SessionID: "mine"}
	theirs := Ref{Kind: TaskKind, ID: "1", SessionID: "theirs"}
	record := createTestContext(t, s, "About task one here", "Only this chat's first task.", source, mine)
	found, err := s.ContextFor(ctx, []Ref{theirs})
	if err != nil || len(found) != 0 {
		t.Fatalf("another chat's task number matched: %v, %v", contextIDs(found), err)
	}
	found, err = s.ContextFor(ctx, []Ref{mine})
	if err != nil || len(found) != 1 || found[0].ID != record.ID {
		t.Fatalf("the addressed task lost its context: %v, %v", contextIDs(found), err)
	}
	// An empty question is an empty answer, not everything that was ever written.
	found, err = s.ContextFor(ctx, nil)
	if err != nil || found == nil || len(found) != 0 {
		t.Fatalf("asking about nothing returned %v, %v", contextIDs(found), err)
	}
}

// The answer is ordered by when records were created, so it does not depend on
// which target matched or on the order the caller happened to ask in.
func TestContextForOrdersByCreationWhicheverWayItIsAsked(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "order.db"))
	source := Ref{Kind: ConversationKind, ID: "origin"}
	first := Ref{Kind: ConversationKind, ID: "first"}
	second := Ref{Kind: StandingKind, ID: "second"}
	a := createTestContext(t, s, "First written", "One.", source, first)
	b := createTestContext(t, s, "Second written", "Two.", source, second, first)
	c := createTestContext(t, s, "Third written", "Three.", source, second)
	want := []string{a.ID, b.ID, c.ID}
	for _, question := range [][]Ref{{first, second}, {second, first}} {
		found, err := s.ContextFor(ctx, question)
		if err != nil || !reflect.DeepEqual(contextIDs(found), want) {
			t.Fatalf("asked %v: %v, %v", question, contextIDs(found), err)
		}
	}
}

// A withdrawn record is not current anywhere, and it is still a record: its
// wording and its provenance stay readable so a person can see what was said
// and that it was taken back.
func TestWithdrawnContextLeavesEveryQueryButKeepsItsHistory(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "withdraw.db"))
	target := Ref{Kind: ConversationKind, ID: "chat"}
	source := Ref{Kind: ConversationKind, ID: "origin"}
	record := createTestContext(t, s, "Provisional finding", "The cache is the cause.", source, target)
	withdrawn, err := s.WithdrawContext(ctx, record.ID, 1)
	if err != nil || !withdrawn.Withdrawn || withdrawn.Revision != 2 {
		t.Fatalf("withdraw: %+v, %v", withdrawn, err)
	}
	if withdrawn.Text != record.Text || !reflect.DeepEqual(withdrawn.Targets, record.Targets) {
		t.Fatalf("the withdrawal lost what it withdrew: %+v", withdrawn)
	}
	found, err := s.ContextFor(ctx, []Ref{target})
	if err != nil || len(found) != 0 {
		t.Fatalf("a withdrawn record is still current: %v, %v", contextIDs(found), err)
	}
	current, err := s.Context(ctx, record.ID)
	if err != nil || !current.Withdrawn {
		t.Fatalf("asking by identity must say it was withdrawn: %+v, %v", current, err)
	}
	// Withdrawing again is the same state, not another revision.
	again, err := s.WithdrawContext(ctx, record.ID, 2)
	if err != nil || again.Revision != 2 {
		t.Fatalf("a repeated withdrawal moved the record: %+v, %v", again, err)
	}
	// Revising a withdrawn record is the explicit way back to current.
	revived, err := s.ReviseContext(ctx, record.ID, 2, "Confirmed finding", "The cache is the cause.", source, []Ref{target})
	if err != nil || revived.Withdrawn || revived.Revision != 3 {
		t.Fatalf("revise after withdrawal: %+v, %v", revived, err)
	}
	found, err = s.ContextFor(ctx, []Ref{target})
	if err != nil || len(found) != 1 || found[0].Revision != 3 {
		t.Fatalf("the revived record is not current: %v, %v", found, err)
	}
	history, err := s.ContextHistory(ctx, record.ID)
	if err != nil || len(history) != 3 {
		t.Fatalf("history %v, %v", history, err)
	}
	for i, want := range []bool{false, true, false} {
		if history[i].Revision != i+1 || history[i].Withdrawn != want {
			t.Fatalf("revision %d reads %+v", i+1, history[i])
		}
	}
}

// Provenance belongs to the revision, because a correction can be a correction
// about where something came from. History is the only place that answers
// whether a current wording is what the original source actually said.
func TestHistoryKeepsEveryRevisionsOwnSource(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "provenance.db"))
	target := Ref{Kind: StandingKind, ID: "email-watch"}
	speculation := Ref{Kind: ConversationKind, ID: "ideation"}
	measurement := Ref{Kind: TaskKind, ID: "9", SessionID: "benchmark"}
	record := createTestContext(t, s, "Import is slow", "Someone thought it took an hour.", speculation, target)
	if _, err := s.ReviseContext(ctx, record.ID, 1, "Import takes 12 minutes", "Measured, not guessed.", measurement, []Ref{target}); err != nil {
		t.Fatal(err)
	}
	history, err := s.ContextHistory(ctx, record.ID)
	if err != nil || len(history) != 2 {
		t.Fatalf("history %v, %v", history, err)
	}
	if history[0].Source != speculation || history[1].Source != measurement {
		t.Fatalf("sources were flattened: %+v", history)
	}
	if history[0].Text != "Someone thought it took an hour." {
		t.Fatalf("an old revision was rewritten: %+v", history[0])
	}
	if _, err := s.ContextHistory(ctx, "no-such-record"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("history of a record that does not exist: %v", err)
	}
	if _, err := s.Context(ctx, "no-such-record"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reading a record that does not exist: %v", err)
	}
}

// TWO EDITORS, ONE WINNER, NO SILENT MERGE. An editor working from a revision
// that has moved is refused whole, and the refusal leaves the record exactly as
// the winner left it.
func TestAStaleEditorIsRefusedAndLosesNothingOfTheWinnersWrite(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "conflict.db"))
	target := Ref{Kind: ConversationKind, ID: "chat"}
	source := Ref{Kind: ConversationKind, ID: "origin"}
	record := createTestContext(t, s, "Original", "First wording.", source, target)
	if _, err := s.ReviseContext(ctx, record.ID, 1, "Winner", "Second wording.", source, []Ref{target}); err != nil {
		t.Fatal(err)
	}
	for _, stale := range []int{0, 1, 3} {
		if _, err := s.ReviseContext(ctx, record.ID, stale, "Loser", "Lost wording.", source, []Ref{target}); !errors.Is(err, ErrConflict) {
			t.Fatalf("revise at %d: %v", stale, err)
		}
		if _, err := s.WithdrawContext(ctx, record.ID, stale); !errors.Is(err, ErrConflict) {
			t.Fatalf("withdraw at %d: %v", stale, err)
		}
	}
	current, err := s.Context(ctx, record.ID)
	if err != nil || current.Revision != 2 || current.Title != "Winner" {
		t.Fatalf("a refused edit reached the record: %+v, %v", current, err)
	}
	history, err := s.ContextHistory(ctx, record.ID)
	if err != nil || len(history) != 2 {
		t.Fatalf("a refused edit left a revision behind: %v, %v", history, err)
	}
	if _, err := s.ReviseContext(ctx, "no-such-record", 1, "Title", "Text.", source, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revising a record that does not exist: %v", err)
	}
	if _, err := s.WithdrawContext(ctx, "no-such-record", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("withdrawing a record that does not exist: %v", err)
	}
}

// Two processes revising the same record at the same moment is the ordinary
// case in a shared checkout. Exactly one may win; the other must be told.
func TestConcurrentRevisionsOfOneRecordDoNotLoseAnUpdate(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "race.db")
	first, second := openTestStore(t, path), openTestStore(t, path)
	target := Ref{Kind: ConversationKind, ID: "chat"}
	source := Ref{Kind: ConversationKind, ID: "origin"}
	record := createTestContext(t, first, "Original", "First wording.", source, target)
	start := make(chan struct{})
	errs := make(chan error, 2)
	for i, s := range []*Store{first, second} {
		go func(i int, s *Store) {
			<-start
			_, err := s.ReviseContext(ctx, record.ID, 1, fmt.Sprintf("Editor %d", i), "New wording.", source, []Ref{target})
			errs <- err
		}(i, s)
	}
	close(start)
	e1, e2 := <-errs, <-errs
	won := 0
	for _, err := range []error{e1, e2} {
		if err == nil {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("both editors were told they won: %v, %v", e1, e2)
	}
	current, err := first.Context(ctx, record.ID)
	if err != nil || current.Revision != 2 || !strings.HasPrefix(current.Title, "Editor ") {
		t.Fatalf("the record does not hold exactly one winner: %+v, %v", current, err)
	}
	history, err := first.ContextHistory(ctx, record.ID)
	if err != nil || len(history) != 2 {
		t.Fatalf("two writes landed where one may: %v, %v", history, err)
	}
}

// The bounds are refusals, not truncations, and a refusal must leave the store
// exactly as it was — including the record a bad revision was aimed at.
func TestMalformedContextIsRefusedWholeAndStoresNothing(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "invalid.db"))
	inbox := createTestCollection(t, s, "Inbox")
	target := Ref{Kind: CollectionKind, ID: inbox.ID}
	source := Ref{Kind: ConversationKind, ID: "origin"}
	kept := createTestContext(t, s, "Kept", "Kept wording.", source, target)
	tooManyTargets := make([]Ref, 0, maxContextTargets+1)
	for i := 0; i <= maxContextTargets; i++ {
		tooManyTargets = append(tooManyTargets, Ref{Kind: ConversationKind, ID: fmt.Sprintf("chat-%d", i)})
	}
	for name, bad := range map[string]struct {
		title, text string
		source      Ref
		targets     []Ref
	}{
		"empty title":         {"", "Text.", source, []Ref{target}},
		"padded title":        {" Title ", "Text.", source, []Ref{target}},
		"newline in title":    {"Two\nlines", "Text.", source, []Ref{target}},
		"long title":          {strings.Repeat("x", maxContextTitle+1), "Text.", source, []Ref{target}},
		"empty text":          {"Title", "", source, []Ref{target}},
		"padded text":         {"Title", "\nText.\n", source, []Ref{target}},
		"escape in text":      {"Title", "Text.\x1b[31m", source, []Ref{target}},
		"long text":           {"Title", strings.Repeat("x", maxContextText+1), source, []Ref{target}},
		"collection source":   {"Title", "Text.", Ref{Kind: CollectionKind, ID: inbox.ID}, []Ref{target}},
		"standing source":     {"Title", "Text.", Ref{Kind: StandingKind, ID: "watch"}, []Ref{target}},
		"unaddressed source":  {"Title", "Text.", Ref{Kind: TaskKind, ID: "1"}, []Ref{target}},
		"unknown source kind": {"Title", "Text.", Ref{Kind: "worker", ID: "x"}, []Ref{target}},
		"invalid target":      {"Title", "Text.", source, []Ref{{Kind: ArtifactKind, ID: "relative.md"}}},
		"too many targets":    {"Title", "Text.", source, tooManyTargets},
	} {
		if _, err := s.CreateContext(ctx, bad.title, bad.text, bad.source, bad.targets); !errors.Is(err, ErrInvalid) {
			t.Errorf("create with %s: %v", name, err)
		}
		if _, err := s.ReviseContext(ctx, kept.ID, 1, bad.title, bad.text, bad.source, bad.targets); !errors.Is(err, ErrInvalid) {
			t.Errorf("revise with %s: %v", name, err)
		}
	}
	// A collection target is the one target this store can vouch for, so it must
	// exist. Every other kind may be offline, archived or not created yet.
	missing := []Ref{{Kind: CollectionKind, ID: "no-such-collection"}}
	if _, err := s.CreateContext(ctx, "Title", "Text.", source, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("create against a missing collection: %v", err)
	}
	if _, err := s.ReviseContext(ctx, kept.ID, 1, "Title", "Text.", source, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("revise onto a missing collection: %v", err)
	}
	unavailable := []Ref{
		{Kind: ConversationKind, ID: "closed-chat"},
		{Kind: TaskKind, ID: "3", SessionID: "finished-chat"},
		{Kind: StandingKind, ID: "paused-watch"},
		{Kind: ArtifactKind, ID: filepath.Join(t.TempDir(), "not-mounted.md")},
	}
	if _, err := s.CreateContext(ctx, "About offline work", "Still worth recording.", source, unavailable); err != nil {
		t.Errorf("refused an unavailable but well-formed target: %v", err)
	}
	// THE REFUSED REVISIONS MUST NOT HAVE MOVED THE RECORD THEY AIMED AT.
	current, err := s.Context(ctx, kept.ID)
	if err != nil || current.Revision != 1 || current.Title != "Kept" || !reflect.DeepEqual(current.Targets, []Ref{target}) {
		t.Fatalf("a refused revision changed the record: %+v, %v", current, err)
	}
	history, err := s.ContextHistory(ctx, kept.ID)
	if err != nil || len(history) != 1 {
		t.Fatalf("a refused revision was written: %v, %v", history, err)
	}
	if _, err := s.ContextFor(ctx, []Ref{{Kind: TaskKind, ID: "1"}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("asked with a malformed target: %v", err)
	}
	if _, err := s.ContextFor(ctx, tooManyTargets); !errors.Is(err, ErrInvalid) {
		t.Fatalf("asked about more targets than the bound: %v", err)
	}
}

// A repeated target is the same statement of applicability twice. It collapses
// to one, keeping the place its first mention had.
func TestRepeatedTargetsCollapseAndKeepTheirFirstPlace(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "dedup.db"))
	source := Ref{Kind: ConversationKind, ID: "origin"}
	first := Ref{Kind: ConversationKind, ID: "first"}
	second := Ref{Kind: StandingKind, ID: "second"}
	record := createTestContext(t, s, "Title", "Text.", source, first, second, first)
	if !reflect.DeepEqual(record.Targets, []Ref{first, second}) {
		t.Fatalf("targets %v", record.Targets)
	}
	stored, err := s.Context(ctx, record.ID)
	if err != nil || !reflect.DeepEqual(stored.Targets, []Ref{first, second}) {
		t.Fatalf("stored targets %v, %v", stored.Targets, err)
	}
	found, err := s.ContextFor(ctx, []Ref{first, first})
	if err != nil || len(found) != 1 {
		t.Fatalf("a repeated question duplicated the answer: %v, %v", contextIDs(found), err)
	}
}

// Every door must reach a cancelled caller as a refusal rather than as a
// partial write, because a surface routinely cancels on a keystroke.
func TestEveryContextDoorRefusesACancelledCallerWithoutWriting(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "cancel.db"))
	live := context.Background()
	target := Ref{Kind: ConversationKind, ID: "chat"}
	source := Ref{Kind: ConversationKind, ID: "origin"}
	record := createTestContext(t, s, "Title", "Text.", source, target)
	ctx, cancel := context.WithCancel(live)
	cancel()
	doors := map[string]func() error{
		"CreateContext": func() error {
			_, err := s.CreateContext(ctx, "Cancelled", "Text.", source, []Ref{target})
			return err
		},
		"ReviseContext": func() error {
			_, err := s.ReviseContext(ctx, record.ID, 1, "Cancelled", "Text.", source, []Ref{target})
			return err
		},
		"WithdrawContext": func() error { _, err := s.WithdrawContext(ctx, record.ID, 1); return err },
		"Context":         func() error { _, err := s.Context(ctx, record.ID); return err },
		"ContextHistory":  func() error { _, err := s.ContextHistory(ctx, record.ID); return err },
		"ContextFor":      func() error { _, err := s.ContextFor(ctx, []Ref{target}); return err },
		"ContextPage": func() error {
			_, err := s.ContextPage(ctx, []Ref{target}, true, 0, 10)
			return err
		},
		"ContextHistoryPage": func() error { _, err := s.ContextHistoryPage(ctx, record.ID, 0, 10); return err },
		"ContextAt":          func() error { _, err := s.ContextAt(ctx, record.ID, 1); return err },
	}
	for name, door := range doors {
		if err := door(); !errors.Is(err, context.Canceled) {
			t.Errorf("%s answered a cancelled caller with %v", name, err)
		}
	}
	found, err := s.ContextFor(live, []Ref{target})
	if err != nil || len(found) != 1 || found[0].ID != record.ID || found[0].Revision != 1 {
		t.Fatalf("a cancelled call changed the store: %v, %v", found, err)
	}
	history, err := s.ContextHistory(live, record.ID)
	if err != nil || len(history) != 1 {
		t.Fatalf("a cancelled call left a revision: %v, %v", history, err)
	}
}

// versionOneSchema is frozen: it is what version 1 shipped, not what the current
// constant happens to say. An upgrade test that reads today's schema would keep
// passing while the upgrade path it claims to cover stopped being exercised.
const versionOneSchema = `
CREATE TABLE collections (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 name TEXT NOT NULL
);
CREATE TABLE memberships (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 collection_id TEXT NOT NULL REFERENCES collections(id),
 kind TEXT NOT NULL CHECK(kind IN ('collection','conversation','task','standing','artifact')),
 ref_id TEXT NOT NULL,
 session_id TEXT NOT NULL,
 target_collection TEXT REFERENCES collections(id),
 CHECK ((kind='collection' AND target_collection IS NOT NULL AND target_collection=ref_id)
     OR (kind!='collection' AND target_collection IS NULL)),
 UNIQUE(collection_id,kind,ref_id,session_id)
);
CREATE INDEX memberships_reference ON memberships(kind,ref_id,session_id);
`

func writeRawDatabase(t *testing.T, path, statements string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(statements); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func readUserVersion(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

// AN UPGRADE ADDS; IT DOES NOT REBUILD. A person's existing folders and
// memberships are the thing most easily lost here, so the version 1 rows are
// read back through the ordinary doors after the store has become version 2.
func TestUpgradingAVersionOneStoreKeepsEveryCollectionAndMembership(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v1.db")
	writeRawDatabase(t, path, fmt.Sprintf(`%s
INSERT INTO collections(id,name) VALUES ('product','Product'),('marketing','Marketing');
INSERT INTO memberships(collection_id,kind,ref_id,session_id,target_collection)
 VALUES ('product','conversation','old-chat','',NULL),
        ('product','task','1','old-chat',NULL),
        ('marketing','collection','product','','product');
PRAGMA application_id=%d; PRAGMA user_version=1`, versionOneSchema, applicationID))
	s := openTestStore(t, path)
	if got := readUserVersion(t, path); got != schemaVersion {
		t.Fatalf("the store is at version %d after opening", got)
	}
	collections, err := s.Collections(ctx)
	if err != nil || len(collections) != 2 || collections[0].Name != "Product" || collections[1].Name != "Marketing" {
		t.Fatalf("collections %v, %v", collections, err)
	}
	members, err := s.Members(ctx, "product")
	want := []Ref{{Kind: ConversationKind, ID: "old-chat"}, {Kind: TaskKind, ID: "1", SessionID: "old-chat"}}
	if err != nil || !reflect.DeepEqual(members, want) {
		t.Fatalf("memberships %v, %v", members, err)
	}
	members, err = s.Members(ctx, "marketing")
	if err != nil || !reflect.DeepEqual(members, []Ref{{Kind: CollectionKind, ID: "product"}}) {
		t.Fatalf("nesting %v, %v", members, err)
	}
	// The upgraded store is a working version 2 store, not merely a stamped one.
	record := createTestContext(t, s, "First shared note", "Written after the upgrade.",
		Ref{Kind: ConversationKind, ID: "old-chat"}, Ref{Kind: CollectionKind, ID: "product"})
	found, err := s.ContextFor(ctx, []Ref{{Kind: CollectionKind, ID: "product"}})
	if err != nil || len(found) != 1 || found[0].ID != record.ID {
		t.Fatalf("the upgraded store cannot hold context: %v, %v", contextIDs(found), err)
	}
	// Opening an already upgraded store is not a second upgrade.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTestStore(t, path)
	again, err := reopened.ContextFor(ctx, []Ref{{Kind: CollectionKind, ID: "product"}})
	if err != nil || len(again) != 1 || again[0].ID != record.ID {
		t.Fatalf("reopening lost the record: %v, %v", contextIDs(again), err)
	}
	if got := readUserVersion(t, path); got != schemaVersion {
		t.Fatalf("the second open moved the version to %d", got)
	}
}

// A store that claims a version it does not have is the dangerous case, because
// the obvious repair is to create the missing tables and carry on — which is
// how somebody's collections quietly become an empty folder list. Every one of
// these must be refused with the file left exactly as it was found.
func TestOpenRefusesDamagedUpgradedAndUnknownVersionsWithoutRewritingThem(t *testing.T) {
	for _, fixture := range []struct{ name, sql string }{
		{"version one missing memberships", fmt.Sprintf(`CREATE TABLE collections (
 seq INTEGER PRIMARY KEY AUTOINCREMENT, id TEXT NOT NULL UNIQUE, name TEXT NOT NULL);
PRAGMA application_id=%d; PRAGMA user_version=1`, applicationID)},
		{"version two missing contexts", fmt.Sprintf(`%s
PRAGMA application_id=%d; PRAGMA user_version=2`, versionOneSchema, applicationID)},
		{"version two missing targets", fmt.Sprintf(`%s
CREATE TABLE contexts (seq INTEGER PRIMARY KEY AUTOINCREMENT, id TEXT NOT NULL UNIQUE, revision INTEGER NOT NULL);
CREATE TABLE context_revisions (seq INTEGER PRIMARY KEY AUTOINCREMENT, context_id TEXT NOT NULL,
 revision INTEGER NOT NULL, title TEXT NOT NULL, text TEXT NOT NULL, source_kind TEXT NOT NULL,
 source_id TEXT NOT NULL, source_session TEXT NOT NULL, withdrawn INTEGER NOT NULL);
PRAGMA application_id=%d; PRAGMA user_version=2`, versionOneSchema, applicationID)},
		{"newer than this binary", fmt.Sprintf(`%s
PRAGMA application_id=%d; PRAGMA user_version=3`, versionOneSchema, applicationID)},
		{"another feature's database", "CREATE TABLE memories(id INTEGER)"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "refused.db")
			writeRawDatabase(t, path, fixture.sql)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if s, err := Open(path); err == nil {
				_ = s.Close()
				t.Fatal("accepted an incompatible store")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("rewrote a store it had refused: %v", err)
			}
		})
	}
}

// A version 1 store whose collections are intact but whose upgrade is
// interrupted must still be a version 1 store afterwards. The upgrade is one
// transaction, so a failure inside it leaves the version stamp where it was.
func TestAFailedUpgradeLeavesAWorkingVersionOneStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "interrupted.db")
	// A table the upgrade needs to create is already there under a foreign shape,
	// so creating the version 2 schema fails part-way through.
	writeRawDatabase(t, path, fmt.Sprintf(`%s
INSERT INTO collections(id,name) VALUES ('kept','Kept');
CREATE TABLE context_targets (something TEXT);
PRAGMA application_id=%d; PRAGMA user_version=1`, versionOneSchema, applicationID))
	if s, err := Open(path); err == nil {
		_ = s.Close()
		t.Fatal("upgraded onto a table it could not create")
	}
	if got := readUserVersion(t, path); got != 1 {
		t.Fatalf("a failed upgrade stamped version %d", got)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var name string
	if err := db.QueryRow("SELECT name FROM collections WHERE id='kept'").Scan(&name); err != nil || name != "Kept" {
		t.Fatalf("a failed upgrade lost the version 1 rows: %q, %v", name, err)
	}
	var partial int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name IN ('contexts','context_revisions')").Scan(&partial); err != nil {
		t.Fatal(err)
	}
	if partial != 0 {
		t.Fatalf("a failed upgrade left %d half-created tables", partial)
	}
}

// Several sessions share one home, so the first open after an update is
// routinely a race between processes that all want to upgrade.
func TestConcurrentOpensUpgradeAVersionOneStoreExactlyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "race-upgrade.db")
	writeRawDatabase(t, path, fmt.Sprintf(`%s
INSERT INTO collections(id,name) VALUES ('kept','Kept');
PRAGMA application_id=%d; PRAGMA user_version=1`, versionOneSchema, applicationID))
	const handles = 8
	var wg sync.WaitGroup
	failures := make([]error, handles)
	for i := 0; i < handles; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, err := Open(path)
			if err != nil {
				failures[i] = err
				return
			}
			defer s.Close()
			_, failures[i] = s.CreateContext(context.Background(), fmt.Sprintf("Note %d", i), "Text.",
				Ref{Kind: ConversationKind, ID: "origin"}, []Ref{{Kind: CollectionKind, ID: "kept"}})
		}(i)
	}
	wg.Wait()
	for i, err := range failures {
		if err != nil {
			t.Errorf("handle %d lost the upgrade race: %v", i, err)
		}
	}
	s := openTestStore(t, path)
	found, err := s.ContextFor(context.Background(), []Ref{{Kind: CollectionKind, ID: "kept"}})
	if err != nil || len(found) != handles {
		t.Fatalf("the racing upgrade kept %d of %d records: %v", len(found), handles, err)
	}
}
