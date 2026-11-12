package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestApplicableMemoryCountsScopeAndRevisionWithoutHydratingLegacyRows(t *testing.T) {
	s := memoryStore(t)
	user := mustWrite(t, s, WriteRequest{Owner: OwnerUser, Text: "user line"})
	mustWrite(t, s, WriteRequest{Owner: OwnerMachine, Text: "machine line"})
	mustWrite(t, s, WriteRequest{Owner: OwnerProject("actual"), Text: "project line"})
	mustWrite(t, s, WriteRequest{Owner: OwnerProject("other"), Text: "foreign line"})
	gone := mustWrite(t, s, WriteRequest{Owner: OwnerUser, Text: "forgotten line"})
	if err := s.ForgetMemory(gone.ID); err != nil {
		t.Fatal(err)
	}
	// An earlier store can contain text far outside today's write limit and bad
	// legacy metadata. A count must not select/decode either field.
	if _, err := s.db.Exec(`UPDATE memories SET text=zeroblob(8388608), tags='not-json' WHERE id=?`, user.ID); err != nil {
		t.Fatal(err)
	}
	owners := []string{OwnerUser, OwnerMachine, OwnerProject("actual")}
	got, err := s.MemoryCountsFor(context.Background(), owners)
	if err != nil {
		t.Fatal(err)
	}
	want := []MemoryScopeCount{{OwnerUser, 1}, {OwnerMachine, 1}, {OwnerProject("actual"), 1}}
	if !reflect.DeepEqual(got.Scopes, want) || len(got.Revision) != 64 {
		t.Fatalf("snapshot=%+v", got)
	}
	again, err := s.MemoryCountsFor(context.Background(), owners)
	if err != nil || !reflect.DeepEqual(again, got) {
		t.Fatalf("read changed snapshot: %+v, %v", again, err)
	}
	// Restore the fixture before exercising a normal write: the real write door correctly refuses an overfull legacy block.
	if _, err := s.db.Exec(`UPDATE memories SET text='user line', tags='[]' WHERE id=?`, user.ID); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, s, WriteRequest{Owner: OwnerMachine, Text: "second machine line"})
	newer, err := s.MemoryCountsFor(context.Background(), owners)
	if err != nil {
		t.Fatal(err)
	}
	if newer.Revision == got.Revision || newer.Scopes[1].Count != 2 {
		t.Fatalf("write absent: %+v", newer)
	}
}

func TestApplicableMemoryCountsRefusesUnknownOwnersAndNeverConvertsCancellationToZero(t *testing.T) {
	s := memoryStore(t)
	for _, owners := range [][]string{nil, {}, {OwnerLegacyProject}, {OwnerUser, OwnerUser}, {"team:1"}, {"project:"}, {"project:" + strings.Repeat("x", 256)}, {OwnerUser, OwnerMachine, OwnerProject("a"), OwnerProject("b")}} {
		if got, err := s.MemoryCountsFor(context.Background(), owners); err == nil || got.Scopes != nil || got.Revision != "" {
			t.Fatalf("accepted %q: %+v %v", owners, got, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := s.MemoryCountsFor(ctx, []string{OwnerUser})
	if !errors.Is(err, context.Canceled) || got.Scopes != nil || got.Revision != "" {
		t.Fatalf("cancel became counts: %+v %v", got, err)
	}
	if got, err := s.MemoryCountsFor(nil, []string{OwnerUser}); err == nil || got.Scopes != nil {
		t.Fatalf("nil context: %+v %v", got, err)
	}
}
