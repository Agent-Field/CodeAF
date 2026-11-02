package syncsetup

import (
	"os"
	"testing"
)

// Contract E2 clears a released hold without withdrawing another writer's fence.
func TestDepartingWriterClearsOnlyItsOwnSendHold(t *testing.T) {
	s := &Sync{Home: t.TempDir(), Ledger: "fixture"}
	first, next := sendLease{Cell: "chat", Fence: 1}, sendLease{Cell: "chat", Fence: 2}
	mustNil(t, s.recordSendLease("chat", first))
	mustNil(t, s.recordSendLease("chat", next))
	mustNil(t, s.forgetSendLease("chat", first))
	_, fence, err := s.SendLease("chat")
	mustNil(t, err)
	if fence != next.Fence {
		t.Fatal("an old writer withdrew a newer fence")
	}
	mustNil(t, s.forgetSendLease("chat", next))
	if _, _, err := s.SendLease("chat"); !os.IsNotExist(err) {
		t.Fatalf("released hold survived: %v", err)
	}
}
