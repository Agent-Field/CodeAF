package workspace

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExpectedRevisionMismatchReturnsErrConflict(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "cas.db"))
	inbox := createTestCollection(t, s, "Inbox")
	chat := Ref{Kind: ConversationKind, ID: "chat"}
	beforeRev := revisionOf(t, s, inbox.ID)
	beforeRoot, _, _ := mustRoot(t, s)

	err := s.AddWith(ctx, inbox.ID, chat, Provenance{Origin: OriginPerson, ExpectedRevision: beforeRev + 7})
	requireConflict(t, err)
	if got, err := s.Members(ctx, inbox.ID); err != nil || len(got) != 0 {
		t.Fatalf("mismatch add wrote a membership: %v, %v", got, err)
	}
	if revisionOf(t, s, inbox.ID) != beforeRev {
		t.Fatal("mismatch add bumped the collection revision")
	}
	if root, _, _ := mustRoot(t, s); root != beforeRoot {
		t.Fatal("mismatch add bumped root_state")
	}

	if err := s.AddWith(ctx, inbox.ID, chat, Provenance{Origin: OriginPerson}); err != nil {
		t.Fatal(err)
	}
	if revisionOf(t, s, inbox.ID) == beforeRev {
		t.Fatal("unconditional add left the collection revision unchanged")
	}
	afterAdd := revisionOf(t, s, inbox.ID)
	other := Ref{Kind: ConversationKind, ID: "other"}
	if err := s.AddWith(ctx, inbox.ID, other, Provenance{Origin: OriginPerson, ExpectedRevision: afterAdd}); err != nil {
		t.Fatal(err)
	}
	stale := Provenance{Origin: OriginPerson, ExpectedRevision: afterAdd}
	if err := s.AddWith(ctx, inbox.ID, Ref{Kind: ConversationKind, ID: "third"}, stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale add: %v", err)
	}
	members, err := s.Members(ctx, inbox.ID)
	if err != nil || len(members) != 2 {
		t.Fatalf("stale add mutated members: %v, %v", members, err)
	}

	removeRev := revisionOf(t, s, inbox.ID)
	requireConflict(t, s.RemoveWith(ctx, inbox.ID, chat, Provenance{Origin: OriginPerson, ExpectedRevision: removeRev + 1}))
	if got, err := s.Members(ctx, inbox.ID); err != nil || len(got) != 2 {
		t.Fatalf("stale remove mutated members: %v, %v", got, err)
	}
	if err := s.RemoveWith(ctx, inbox.ID, chat, Provenance{Origin: OriginPerson, ExpectedRevision: revisionOf(t, s, inbox.ID)}); err != nil {
		t.Fatal(err)
	}
}

func TestMoveOneSideMismatchLeavesBothEdgesUnchanged(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "move-cas.db"))
	billing := createTestCollection(t, s, "Billing")
	receipts := createTestCollection(t, s, "Receipts")
	security := createTestCollection(t, s, "Security")
	chat := Ref{Kind: ConversationKind, ID: "shared"}
	if err := s.Add(ctx, billing.ID, chat); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(ctx, security.ID, chat); err != nil {
		t.Fatal(err)
	}
	before := snapshotMove(t, s, billing.ID, receipts.ID, security.ID, chat)

	wrongFrom := Provenance{Origin: OriginPerson, ExpectedFrom: before.fromRev + 1, ExpectedTo: before.toRev}
	requireConflict(t, s.Move(ctx, billing.ID, receipts.ID, chat, wrongFrom))
	assertMoveUnchanged(t, s, billing.ID, receipts.ID, security.ID, chat, before)

	wrongTo := Provenance{Origin: OriginPerson, ExpectedFrom: before.fromRev, ExpectedTo: before.toRev + 1}
	requireConflict(t, s.Move(ctx, billing.ID, receipts.ID, chat, wrongTo))
	assertMoveUnchanged(t, s, billing.ID, receipts.ID, security.ID, chat, before)

	ok := Provenance{Origin: OriginPerson, ExpectedFrom: before.fromRev, ExpectedTo: before.toRev}
	if err := s.Move(ctx, billing.ID, receipts.ID, chat, ok); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Members(ctx, billing.ID); err != nil || len(got) != 0 {
		t.Fatalf("matching move left the source: %v, %v", got, err)
	}
	if got, err := s.Members(ctx, receipts.ID); err != nil || len(got) != 1 || got[0] != chat {
		t.Fatalf("matching move missed the destination: %v, %v", got, err)
	}
	if got, err := s.Members(ctx, security.ID); err != nil || len(got) != 1 || got[0] != chat {
		t.Fatalf("matching move dropped the other placement: %v, %v", got, err)
	}
}

func TestRootStateIncrementsOnMutatingWrites(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "root-rev.db")
	s := openTestStore(t, path)
	if _, _, _, err := s.RootState(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("blank root state: %v", err)
	}
	if s.SchemaVersion() != 0 {
		t.Fatalf("RootState initialized a blank file to version %d", s.SchemaVersion())
	}
	if reopen := openTestStore(t, path); reopen.SchemaVersion() != 0 {
		t.Fatalf("RootState migrated the file to version %d", reopen.SchemaVersion())
	}

	from := createTestCollection(t, s, "From")
	to := createTestCollection(t, s, "To")
	root, purpose, updatedAt := mustRoot(t, s)
	if root < 1 || updatedAt == "" {
		t.Fatalf("root after create: revision %d purpose %q updated %q", root, purpose, updatedAt)
	}
	fromRev := revisionOf(t, s, from.ID)
	chat := Ref{Kind: ConversationKind, ID: "filed"}
	if err := s.Add(ctx, from.ID, chat); err != nil {
		t.Fatal(err)
	}
	afterAdd, _, _ := mustRoot(t, s)
	if afterAdd != root+1 {
		t.Fatalf("add root revision %d, want %d", afterAdd, root+1)
	}
	if revisionOf(t, s, from.ID) != fromRev+1 {
		t.Fatalf("add collection revision %d, want %d", revisionOf(t, s, from.ID), fromRev+1)
	}

	if err := s.Remove(ctx, from.ID, chat); err != nil {
		t.Fatal(err)
	}
	afterRemove, _, _ := mustRoot(t, s)
	if afterRemove != afterAdd+1 {
		t.Fatalf("remove root revision %d, want %d", afterRemove, afterAdd+1)
	}

	if err := s.Add(ctx, from.ID, chat); err != nil {
		t.Fatal(err)
	}
	beforeMove, _, _ := mustRoot(t, s)
	fromBeforeMove := revisionOf(t, s, from.ID)
	toBeforeMove := revisionOf(t, s, to.ID)
	if err := s.Move(ctx, from.ID, to.ID, chat, Provenance{Origin: OriginPerson}); err != nil {
		t.Fatal(err)
	}
	afterMove, _, _ := mustRoot(t, s)
	if afterMove != beforeMove+1 {
		t.Fatalf("move root revision %d, want %d", afterMove, beforeMove+1)
	}
	if revisionOf(t, s, from.ID) != fromBeforeMove+1 || revisionOf(t, s, to.ID) != toBeforeMove+1 {
		t.Fatalf("move collection revisions from=%d to=%d", revisionOf(t, s, from.ID), revisionOf(t, s, to.ID))
	}

	conflictRoot, _, _ := mustRoot(t, s)
	err := s.AddWith(ctx, to.ID, Ref{Kind: ConversationKind, ID: "stale"}, Provenance{Origin: OriginPerson, ExpectedRevision: 999})
	requireConflict(t, err)
	if got, _, _ := mustRoot(t, s); got != conflictRoot {
		t.Fatalf("conflict bumped root from %d to %d", conflictRoot, got)
	}
}

type moveSnap struct {
	fromRev, toRev, extraRev, root       int
	fromMembers, toMembers, extraMembers []Ref
	fromEvents, toEvents                 []MembershipEvent
}

func snapshotMove(t *testing.T, s *Store, fromID, toID, extraID string, ref Ref) moveSnap {
	t.Helper()
	ctx := context.Background()
	fromMembers, err := s.Members(ctx, fromID)
	if err != nil {
		t.Fatal(err)
	}
	toMembers, err := s.Members(ctx, toID)
	if err != nil {
		t.Fatal(err)
	}
	extraMembers, err := s.Members(ctx, extraID)
	if err != nil {
		t.Fatal(err)
	}
	fromEvents, err := s.Events(ctx, fromID, ref)
	if err != nil {
		t.Fatal(err)
	}
	toEvents, err := s.Events(ctx, toID, ref)
	if err != nil {
		t.Fatal(err)
	}
	root, _, _ := mustRoot(t, s)
	return moveSnap{
		fromRev: revisionOf(t, s, fromID), toRev: revisionOf(t, s, toID), extraRev: revisionOf(t, s, extraID),
		root: root, fromMembers: fromMembers, toMembers: toMembers, extraMembers: extraMembers,
		fromEvents: fromEvents, toEvents: toEvents,
	}
}

func assertMoveUnchanged(t *testing.T, s *Store, fromID, toID, extraID string, ref Ref, want moveSnap) {
	t.Helper()
	got := snapshotMove(t, s, fromID, toID, extraID, ref)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("move mismatch mutated state:\n got %+v\nwant %+v", got, want)
	}
}

func mustRoot(t *testing.T, s *Store) (revision int, purpose, updatedAt string) {
	t.Helper()
	revision, purpose, updatedAt, err := s.RootState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return revision, purpose, updatedAt
}

func revisionOf(t *testing.T, s *Store, id string) int {
	t.Helper()
	got, err := s.Collections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got {
		if c.ID == id {
			return c.Revision
		}
	}
	t.Fatalf("collection %s missing", id)
	return 0
}

func requireConflict(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("got %v, want ErrConflict", err)
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("ErrConflict must wrap ErrInvalid: %v", err)
	}
	if !strings.Contains(err.Error(), "revision") {
		t.Fatalf("conflict %q does not name the revision", err)
	}
}
