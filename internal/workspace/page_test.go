package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// A page is a bound on what a caller is handed, not on what the store holds, so
// the caller has to be told the difference. An answer that silently stops at the
// limit reads exactly like an answer that ran out.
func TestAPageStopsAtItsLimitAndSaysThereIsMore(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "page.db"))
	chat := Ref{Kind: ConversationKind, ID: "chat"}
	source := Ref{Kind: ConversationKind, ID: "origin"}
	written := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		written = append(written, createTestContext(t, s, fmt.Sprintf("Finding %d", i), "Text.", source, chat).ID)
	}
	page, err := s.ContextPage(ctx, []Ref{chat}, false, 0, 2)
	if err != nil || !reflect.DeepEqual(contextIDs(page.Records), written[:2]) || !page.More {
		t.Fatalf("first page %v more=%v: %v", contextIDs(page.Records), page.More, err)
	}
	page, err = s.ContextPage(ctx, []Ref{chat}, false, 4, 2)
	if err != nil || !reflect.DeepEqual(contextIDs(page.Records), written[4:]) || page.More {
		t.Fatalf("last page %v more=%v: %v", contextIDs(page.Records), page.More, err)
	}
	// A limit larger than a page is a page, not a refusal, and an unspecified
	// limit is whatever a page holds.
	for _, limit := range []int{0, MaxContextPage + 100} {
		page, err = s.ContextPage(ctx, []Ref{chat}, false, 0, limit)
		if err != nil || !reflect.DeepEqual(contextIDs(page.Records), written) || page.More {
			t.Fatalf("limit %d: %v more=%v, %v", limit, contextIDs(page.Records), page.More, err)
		}
	}
	// Past the end is an empty page that does not claim there is more.
	page, err = s.ContextPage(ctx, []Ref{chat}, false, 99, 2)
	if err != nil || len(page.Records) != 0 || page.More {
		t.Fatalf("past the end: %v more=%v, %v", contextIDs(page.Records), page.More, err)
	}
	// An empty question is an empty answer here too.
	page, err = s.ContextPage(ctx, nil, true, 0, 10)
	if err != nil || page.Records == nil || len(page.Records) != 0 || page.More || page.PreviouslyApplied {
		t.Fatalf("asking about nothing: %+v, %v", page, err)
	}
	for _, bad := range [][2]int{{-1, 2}, {0, -1}} {
		if _, err := s.ContextPage(ctx, []Ref{chat}, false, bad[0], bad[1]); !errors.Is(err, ErrInvalid) {
			t.Errorf("offset %d limit %d: %v", bad[0], bad[1], err)
		}
		if _, err := s.ContextHistoryPage(ctx, written[0], bad[0], bad[1]); !errors.Is(err, ErrInvalid) {
			t.Errorf("history offset %d limit %d: %v", bad[0], bad[1], err)
		}
	}
}

// AN EMPTY PLACE THAT WAS ONCE FULL IS NOT THE SAME AS A PLACE NOBODY WROTE
// ABOUT, and the records alone cannot tell a reader which one they are looking
// at. The flag is the only difference, and it must not fire on a mere rewording.
func TestAPlaceRemembersThatContextWasWithdrawnOrPointedElsewhere(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "previously.db"))
	source := Ref{Kind: ConversationKind, ID: "origin"}
	quiet := Ref{Kind: ConversationKind, ID: "never-written-about"}
	page, err := s.ContextPage(ctx, []Ref{quiet}, false, 0, 10)
	if err != nil || len(page.Records) != 0 || page.PreviouslyApplied {
		t.Fatalf("a place nobody wrote about: %+v, %v", page, err)
	}
	withdrawnPlace := Ref{Kind: ConversationKind, ID: "withdrawn-chat"}
	record := createTestContext(t, s, "Provisional", "The cache is the cause.", source, withdrawnPlace)
	if _, err := s.WithdrawContext(ctx, record.ID, 1); err != nil {
		t.Fatal(err)
	}
	page, err = s.ContextPage(ctx, []Ref{withdrawnPlace}, false, 0, 10)
	if err != nil || len(page.Records) != 0 || !page.PreviouslyApplied {
		t.Fatalf("a withdrawal left no trace: %+v, %v", page, err)
	}
	// A revision that points the record somewhere else is the same loss here,
	// and is no loss at all where it landed.
	from, to := Ref{Kind: StandingKind, ID: "from"}, Ref{Kind: StandingKind, ID: "to"}
	moved := createTestContext(t, s, "Pricing decision", "Annual only for now.", source, from)
	if _, err := s.ReviseContext(ctx, moved.ID, 1, "Pricing decision", "Annual only for now.", source, []Ref{to}); err != nil {
		t.Fatal(err)
	}
	page, err = s.ContextPage(ctx, []Ref{from}, false, 0, 10)
	if err != nil || len(page.Records) != 0 || !page.PreviouslyApplied {
		t.Fatalf("a retarget left no trace where it left: %+v, %v", page, err)
	}
	page, err = s.ContextPage(ctx, []Ref{to}, false, 0, 10)
	if err != nil || len(page.Records) != 1 || page.PreviouslyApplied {
		t.Fatalf("the place it moved to reports a loss it never had: %+v, %v", page, err)
	}
	// A RECORD THAT WAS ONLY REWORDED IS NOT A LOSS. Every revised record has
	// older revisions, so counting those would make the flag mean nothing.
	stays := Ref{Kind: ConversationKind, ID: "stays"}
	kept := createTestContext(t, s, "First wording", "One.", source, stays)
	if _, err := s.ReviseContext(ctx, kept.ID, 1, "Second wording", "Two.", source, []Ref{stays}); err != nil {
		t.Fatal(err)
	}
	page, err = s.ContextPage(ctx, []Ref{stays}, false, 0, 10)
	if err != nil || len(page.Records) != 1 || page.PreviouslyApplied {
		t.Fatalf("a reworded record was reported as taken back: %+v, %v", page, err)
	}
}

// A CHAT MAY SIT IN A HUNDRED FOLDERS AND A FOLDER MAY HOLD THOUSANDS OF CHATS.
// Neither number is the caller's, so neither may be charged against the bound on
// how many places one question names.
func TestAFolderWithManyMembersIsAnsweredWithoutSpendingTheAskersBound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "wide.db"))
	folder := createTestCollection(t, s, "Everything")
	const members = MaxContextTargets*2 + 3
	for i := 0; i < members; i++ {
		if err := s.Add(ctx, folder.ID, Ref{Kind: ConversationKind, ID: fmt.Sprintf("chat-%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	source := Ref{Kind: ConversationKind, ID: "origin"}
	record := createTestContext(t, s, "Applies to the whole folder", "Filed once, against the folder.",
		source, Ref{Kind: CollectionKind, ID: folder.ID})
	for _, i := range []int{0, members - 1} {
		one := []Ref{{Kind: ConversationKind, ID: fmt.Sprintf("chat-%d", i)}}
		page, err := s.ContextPage(ctx, one, true, 0, 10)
		if err != nil || len(page.Records) != 1 || page.Records[0].ID != record.ID {
			t.Fatalf("chat-%d through its folder: %v, %v", i, contextIDs(page.Records), err)
		}
		// Without the hop the same question is empty, because the record names
		// the folder and nothing else.
		page, err = s.ContextPage(ctx, one, false, 0, 10)
		if err != nil || len(page.Records) != 0 {
			t.Fatalf("chat-%d reached a folder's record directly: %v, %v", i, contextIDs(page.Records), err)
		}
	}
	page, err := s.ContextPage(ctx, []Ref{{Kind: ConversationKind, ID: "outside"}}, true, 0, 10)
	if err != nil || len(page.Records) != 0 {
		t.Fatalf("a chat outside the folder: %v, %v", contextIDs(page.Records), err)
	}
	// Membership is consulted as it stands, so leaving the folder ends the reach.
	// It is not remembered as context that used to apply, because the record was
	// never filed against the chat.
	leaving := Ref{Kind: ConversationKind, ID: "chat-0"}
	if err := s.Remove(ctx, folder.ID, leaving); err != nil {
		t.Fatal(err)
	}
	page, err = s.ContextPage(ctx, []Ref{leaving}, true, 0, 10)
	if err != nil || len(page.Records) != 0 || page.PreviouslyApplied {
		t.Fatalf("a chat that left the folder: %+v, %v", page, err)
	}
}

// THE HOP IS ONE HOP AND IT NEVER CLIMBS. A record on an outer folder is not the
// context of a chat two levels inside it, and a record on an inner folder is not
// the outer folder's. Organization is not relevance; a reach that recursed would
// make every note in a person's home apply to everything.
func TestFolderContextReachesOneHopAndNeverAnAncestorsChat(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "hop.db"))
	startup, product := createTestCollection(t, s, "Startup"), createTestCollection(t, s, "Product")
	if err := s.Add(ctx, startup.ID, Ref{Kind: CollectionKind, ID: product.ID}); err != nil {
		t.Fatal(err)
	}
	chat := Ref{Kind: ConversationKind, ID: "product-chat"}
	if err := s.Add(ctx, product.ID, chat); err != nil {
		t.Fatal(err)
	}
	source := Ref{Kind: ConversationKind, ID: "origin"}
	outer := createTestContext(t, s, "Filed against Startup", "The outer folder only.",
		source, Ref{Kind: CollectionKind, ID: startup.ID})
	page, err := s.ContextPage(ctx, []Ref{{Kind: CollectionKind, ID: product.ID}}, true, 0, 10)
	if err != nil || len(page.Records) != 1 || page.Records[0].ID != outer.ID {
		t.Fatalf("the folder's own member: %v, %v", contextIDs(page.Records), err)
	}
	page, err = s.ContextPage(ctx, []Ref{chat}, true, 0, 10)
	if err != nil || len(page.Records) != 0 || page.PreviouslyApplied {
		t.Fatalf("a grandchild inherited context: %+v, %v", page, err)
	}
	inner := createTestContext(t, s, "Filed against Product", "The inner folder only.",
		source, Ref{Kind: CollectionKind, ID: product.ID})
	page, err = s.ContextPage(ctx, []Ref{{Kind: CollectionKind, ID: startup.ID}}, true, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, found := range page.Records {
		if found.ID == inner.ID {
			t.Fatalf("a parent folder read its child's context: %v", contextIDs(page.Records))
		}
	}
	// The chat's own record is still reached directly, hop or no hop.
	own := createTestContext(t, s, "Filed against the chat", "This chat only.", source, chat)
	for _, hop := range []bool{false, true} {
		page, err = s.ContextPage(ctx, []Ref{chat}, hop, 0, 10)
		want := []string{own.ID}
		if hop {
			want = []string{inner.ID, own.ID}
		}
		if err != nil || !reflect.DeepEqual(contextIDs(page.Records), want) {
			t.Fatalf("hop=%v got %v, want %v: %v", hop, contextIDs(page.Records), want, err)
		}
	}
}

// Following a citation means reading the wording as it stood, not the wording as
// it stands, and doing it without dragging every other revision along.
func TestAnExactRevisionIsReadWithoutItsHistory(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "at.db"))
	target := Ref{Kind: ConversationKind, ID: "chat"}
	speculation := Ref{Kind: ConversationKind, ID: "ideation"}
	measurement := Ref{Kind: TaskKind, ID: "9", SessionID: "benchmark"}
	record := createTestContext(t, s, "Import is slow", "Someone thought it took an hour.", speculation, target)
	if _, err := s.ReviseContext(ctx, record.ID, 1, "Import takes 12 minutes", "Measured, not guessed.", measurement, []Ref{target}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WithdrawContext(ctx, record.ID, 2); err != nil {
		t.Fatal(err)
	}
	first, err := s.ContextAt(ctx, record.ID, 1)
	if err != nil || first.Title != "Import is slow" || first.Source != speculation || first.Withdrawn {
		t.Fatalf("revision 1: %+v, %v", first, err)
	}
	second, err := s.ContextAt(ctx, record.ID, 2)
	if err != nil || second.Source != measurement || !reflect.DeepEqual(second.Targets, []Ref{target}) {
		t.Fatalf("revision 2: %+v, %v", second, err)
	}
	current, err := s.ContextAt(ctx, record.ID, 0)
	if err != nil || current.Revision != 3 || !current.Withdrawn {
		t.Fatalf("revision 0 is the current one: %+v, %v", current, err)
	}
	if _, err := s.ContextAt(ctx, record.ID, 4); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a revision that was never written: %v", err)
	}
	if _, err := s.ContextAt(ctx, "no-such-record", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a record that does not exist: %v", err)
	}
	if _, err := s.ContextAt(ctx, record.ID, -1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a negative revision: %v", err)
	}
}

// "There is no such record" and "you have read them all" are different answers,
// and a paged history is where they are easiest to confuse.
func TestHistoryPagesOldestFirstAndAnEmptyPageIsNotAbsence(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "history-page.db"))
	target := Ref{Kind: ConversationKind, ID: "chat"}
	source := Ref{Kind: ConversationKind, ID: "origin"}
	record := createTestContext(t, s, "Wording 1", "One.", source, target)
	for revision := 1; revision < 5; revision++ {
		if _, err := s.ReviseContext(ctx, record.ID, revision, fmt.Sprintf("Wording %d", revision+1), "Text.", source, []Ref{target}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.ContextHistoryPage(ctx, record.ID, 0, 2)
	if err != nil || len(page.Records) != 2 || !page.More {
		t.Fatalf("first page: %+v, %v", page, err)
	}
	if page.Records[0].Revision != 1 || page.Records[1].Revision != 2 {
		t.Fatalf("history is not oldest first: %+v", page.Records)
	}
	page, err = s.ContextHistoryPage(ctx, record.ID, 3, 2)
	if err != nil || len(page.Records) != 2 || page.More {
		t.Fatalf("last page: %+v, %v", page, err)
	}
	if page.Records[1].Revision != 5 || page.Records[1].Title != "Wording 5" {
		t.Fatalf("the newest revision reads %+v", page.Records[1])
	}
	page, err = s.ContextHistoryPage(ctx, record.ID, 99, 2)
	if err != nil || len(page.Records) != 0 || page.More {
		t.Fatalf("past the end of a record that exists: %+v, %v", page, err)
	}
	if _, err := s.ContextHistoryPage(ctx, "no-such-record", 0, 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a record that does not exist: %v", err)
	}
	// The whole history still reads the same way through the unpaged door.
	whole, err := s.ContextHistory(ctx, record.ID)
	if err != nil || len(whole) != 5 || whole[0].Revision != 1 || whole[4].Revision != 5 {
		t.Fatalf("the unpaged history: %v, %v", whole, err)
	}
}

// SEVERAL SESSIONS SHARE ONE HOME, so a peer holding the writer is the ordinary
// case rather than a rare one. Opening a store that is already at this version is
// a read, and a read must not be refused because somebody else is mid-write.
func TestOpeningACurrentStoreDoesNotWaitForAPeersWriter(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reserved.db")
	writer := openTestStore(t, path)
	// This handle's transactions are immediate, so the writer is reserved from
	// here until the rollback below.
	tx, err := writer.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "INSERT INTO collections(id,name) VALUES ('held','Held')"); err != nil {
		t.Fatal(err)
	}
	reader, err := Open(path)
	if err != nil {
		t.Fatalf("an open was refused while a peer held the writer: %v", err)
	}
	defer reader.Close()
	// It reads the committed store, not the writer's uncommitted row.
	collections, err := reader.Collections(ctx)
	if err != nil || len(collections) != 0 {
		t.Fatalf("the open read uncommitted rows: %v, %v", collections, err)
	}
	existing, err := OpenExisting(path)
	if err != nil {
		t.Fatalf("OpenExisting was refused while a peer held the writer: %v", err)
	}
	if err := existing.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
}

// A READ MUST NOT BRING BACK A DATABASE SOMEBODY REMOVED, because handing back an
// empty store is exactly how a person's folders quietly become an empty list.
func TestOpenExistingRefusesAnAbsentStoreWithoutCreatingIt(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "gone", "collections.db")
	if _, err := OpenExisting(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("opening a store that is not there: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a read created the directory it was told to read from")
	}
	made := openTestStore(t, path)
	chat := Ref{Kind: ConversationKind, ID: "chat"}
	record := createTestContext(t, made, "Kept", "Written before the read.",
		Ref{Kind: ConversationKind, ID: "origin"}, chat)
	if err := made.Close(); err != nil {
		t.Fatal(err)
	}
	existing, err := OpenExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	found, err := existing.ContextFor(ctx, []Ref{chat})
	if err != nil || len(found) != 1 || found[0].ID != record.ID {
		t.Fatalf("the existing store did not read back: %v, %v", contextIDs(found), err)
	}
	if err := existing.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenExisting(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("opening a removed store: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a read recreated the removed database")
	}
	if _, err := OpenExisting(""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an empty path: %v", err)
	}
}

// A PLACE NAMED TWICE IS ONE PLACE. Counting the bound before the repeats
// collapse would refuse a legitimate question about half as many places as the
// bound allows.
func TestRepeatedPlacesDoNotSpendTheBoundOnAskingOrWriting(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "bound.db"))
	source := Ref{Kind: ConversationKind, ID: "origin"}
	chat := Ref{Kind: ConversationKind, ID: "chat"}
	record := createTestContext(t, s, "One place", "Text.", source, chat)
	// The bound's worth of distinct places, each of them said twice.
	repeated := make([]Ref, 0, MaxContextTargets*2)
	for i := 0; i < MaxContextTargets; i++ {
		place := Ref{Kind: ConversationKind, ID: fmt.Sprintf("chat-%d", i)}
		repeated = append(repeated, place, place)
	}
	repeated[0], repeated[1] = chat, chat
	found, err := s.ContextFor(ctx, repeated)
	if err != nil || len(found) != 1 || found[0].ID != record.ID {
		t.Fatalf("a repeated question was refused or duplicated: %v, %v", contextIDs(found), err)
	}
	page, err := s.ContextPage(ctx, repeated, false, 0, 10)
	if err != nil || len(page.Records) != 1 || page.Records[0].ID != record.ID {
		t.Fatalf("a repeated page question: %v, %v", contextIDs(page.Records), err)
	}
	stored := createTestContext(t, s, "Many places", "Text.", source, repeated...)
	if len(stored.Targets) != MaxContextTargets {
		t.Fatalf("a record kept %d of %d distinct places", len(stored.Targets), MaxContextTargets)
	}
	// One more DISTINCT place is over the bound and is still refused.
	over := append(append([]Ref{}, repeated...), Ref{Kind: ConversationKind, ID: "one-too-many"})
	if _, err := s.ContextFor(ctx, over); !errors.Is(err, ErrInvalid) {
		t.Fatalf("asked about more distinct places than the bound: %v", err)
	}
	if _, err := s.ContextPage(ctx, over, true, 0, 10); !errors.Is(err, ErrInvalid) {
		t.Fatalf("paged more distinct places than the bound: %v", err)
	}
	if _, err := s.CreateContext(ctx, "Too many", "Text.", source, over); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrote more distinct places than the bound: %v", err)
	}
}
