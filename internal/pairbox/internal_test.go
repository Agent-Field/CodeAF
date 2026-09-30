package pairbox

import (
	"context"
	"testing"
	"time"
)

func TestPlateDigitsGrowWithTheLiveCount(t *testing.T) {
	for _, c := range []struct{ live, want int }{
		{1, 2}, {10, 2}, {11, 3}, {100, 3}, {101, 4}, {2000, 4}, {100000, 4},
	} {
		if got := plateDigits(c.live); got != c.want {
			t.Errorf("plateDigits(%d) = %d, want %d", c.live, got, c.want)
		}
	}
}

// clock is a time the test moves by hand.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestDeleteGivesTheBytesBack(t *testing.T) {
	ctx := context.Background()
	m := NewMemory(DefaultLimits, nil)
	key, _ := NewKey()
	made, err := m.Create(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Post(ctx, made.Nameplate, SideA, key, make([]byte, 100)); err != nil {
		t.Fatal(err)
	}
	if m.bytes != 100 {
		t.Fatalf("the service holds %d bytes after one 100 byte message", m.bytes)
	}
	if err := m.Delete(ctx, made.Nameplate, key); err != nil {
		t.Fatal(err)
	}
	if m.bytes != 0 {
		t.Errorf("the service still counts %d bytes after the delete", m.bytes)
	}
}

func TestExpiryGivesTheBytesBack(t *testing.T) {
	ctx, c := context.Background(), &clock{time.UnixMilli(1)}
	m := NewMemory(DefaultLimits, c.now)
	key, _ := NewKey()
	made, _ := m.Create(ctx, key)
	if _, err := m.Post(ctx, made.Nameplate, SideA, key, make([]byte, 100)); err != nil {
		t.Fatal(err)
	}
	c.t = c.t.Add(DefaultLimits.TTL)
	if _, err := m.Poll(ctx, made.Nameplate, SideA, 0, 0); err != ErrGone {
		t.Fatalf("Poll after the ttl = %v, want ErrGone", err)
	}
	if m.bytes != 0 || len(m.boxes) != 0 {
		t.Errorf("an expired mailbox is still held: %d bytes, %d boxes", m.bytes, len(m.boxes))
	}
}

// A network that has gone quiet is forgotten by the next create, so budgets do
// not accumulate one entry for every address that ever called.
func TestQuietNetworksAreForgotten(t *testing.T) {
	c := &clock{time.UnixMilli(1)}
	m := NewMemory(DefaultLimits, c.now)
	key, _ := NewKey()
	if _, err := m.Create(WithPeer(context.Background(), "old"), key); err != nil {
		t.Fatal(err)
	}
	c.t = c.t.Add(createSpan + time.Second)
	if _, err := m.Create(WithPeer(context.Background(), "new"), key); err != nil {
		t.Fatal(err)
	}
	if _, kept := m.peers["old"]; kept || len(m.peers) != 1 {
		t.Errorf("the service still remembers %d networks, want only the new one", len(m.peers))
	}
}
