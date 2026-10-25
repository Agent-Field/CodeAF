package directory

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestFrameHintsReplaceAndListOmission(t *testing.T) {
	hint := []FrameHint{{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Bytes: 42}}
	c := Created(CellInit{Head: "first", FrameHints: hint}, "device", 1)
	if len(c.FrameHints) != 1 {
		t.Fatal("create lost hints")
	}
	changed, err := PublishTo(c, "device", Publish{Fence: 1, OldHead: "first", Head: "second"}, 2)
	if err != nil || len(changed.FrameHints) != 0 {
		t.Fatalf("old publisher retained stale hints: %v", err)
	}
	listing := withoutFrames(Listing{Cells: map[string]Cell{"c": c}})
	if len(listing.Cells["c"].FrameHints) != 0 {
		t.Fatal("home list carried frame hints")
	}
}

func TestFrameHintsValidateAndCopyInput(t *testing.T) {
	source := []FrameHint{{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Bytes: 65535}}
	c := Created(CellInit{FrameHints: source}, "d", 1)
	source[0].Bytes = 1
	if len(c.FrameHints) != 1 || c.FrameHints[0].Bytes != 65535 {
		t.Fatal("metadata input aliased")
	}
	for _, bad := range [][]FrameHint{{{ID: "a", Bytes: 1}}, {{ID: c.FrameHints[0].ID, Bytes: 65536}}, {{ID: c.FrameHints[0].ID, Bytes: 9223372036854775807}}, {c.FrameHints[0], c.FrameHints[0]}} {
		if got := Created(CellInit{FrameHints: bad}, "d", 1); len(got.FrameHints) != 0 {
			t.Fatal("invalid hint set retained")
		}
	}
}

func TestFrameHintReturnedViewsCannotMutateCommittedMetadata(t *testing.T) {
	m := NewMemory(func() time.Time { return time.Unix(1, 0) })
	client := m.For("d")
	hints := []FrameHint{{ID: strings.Repeat("a", 64), Bytes: 42}}
	v, err := client.Create(context.Background(), "c", CellInit{Head: "h", FrameHints: hints})
	if err != nil {
		t.Fatal(err)
	}
	v.Cell.FrameHints[0].Bytes = 1
	read, err := client.Cell(context.Background(), "c")
	if err != nil {
		t.Fatal(err)
	}
	if read.Cell.FrameHints[0].Bytes != 42 {
		t.Fatal("Create result changed committed hints")
	}
	read.Cell.FrameHints[0].Bytes = 2
	v, err = client.Publish(context.Background(), "c", Publish{Fence: 1, OldHead: "h", Head: "next", FrameHints: hints})
	if err != nil {
		t.Fatal(err)
	}
	v.Cell.FrameHints[0].Bytes = 3
	final, _ := client.Cell(context.Background(), "c")
	if final.Cell.FrameHints[0].Bytes != 42 {
		t.Fatal("Publish result changed committed hints")
	}
}
