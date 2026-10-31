package cellsync

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// C4: doctor repair cannot acquire a record that appeared after its listing,
// even when the other computer has already released an unfamiliar head.
func TestCreateOnlyPublishLeavesExistingHistoryAlone(t *testing.T) {
	r := newRig(t)
	head := r.seal(map[string]string{"local": "only here"})
	other := r.dir.For(devB)
	v, err := other.Create(t.Context(), cellID, directory.CellInit{Head: otherHead, Class: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Release(t.Context(), cellID, v.Cell.Lease.Fence); err != nil {
		t.Fatal(err)
	}
	before, err := other.Cell(t.Context(), cellID)
	if err != nil {
		t.Fatal(err)
	}
	r.pub.CreateOnly = true
	if err := r.pub.Publish(t.Context(), r.drv, head, r.info()); !errors.Is(err, directory.ErrExists) {
		t.Fatalf("publish = %v", err)
	}
	after, err := other.Cell(t.Context(), cellID)
	if err != nil || after.Cell.Head != before.Cell.Head || after.Cell.Lease != before.Cell.Lease {
		t.Fatalf("existing history changed: %+v, %v", after, err)
	}
}
