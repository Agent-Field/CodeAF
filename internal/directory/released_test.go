package directory

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestC31ReleaseClockIsIndependentOfTheLastTurn proves C31.1: quitting today
// can continue even when the chat's last saved content is more than a day old.
func TestC31ReleaseClockIsIndependentOfTheLastTurn(t *testing.T) {
	now := time.Unix(1700000000, 0)
	d := NewMemory(func() time.Time { return now })
	client := d.For("dev_a")
	c, err := client.Create(context.Background(), "01AAAAAAAAAAAAAAAAAAAAAAAA", CellInit{Head: strings.Repeat("a", 64), Class: "host-bound"})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(48 * time.Hour)
	if err := client.Release(context.Background(), "01AAAAAAAAAAAAAAAAAAAAAAAA", c.Cell.Lease.Fence); err != nil {
		t.Fatal(err)
	}
	got, err := client.Cell(context.Background(), "01AAAAAAAAAAAAAAAAAAAAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	if got.Cell.Lease.ReleasedAt != now.UnixMilli() || got.Cell.DurableAt == now.UnixMilli() {
		t.Fatal("quit time was confused with saved content time")
	}
}

// TestC31BackgroundReleaseIsNotAQuit proves C29.2/C31.3: preparation can make
// an old chat available without causing another computer to open it by itself.
func TestC31BackgroundReleaseIsNotAQuit(t *testing.T) {
	c := Created(CellInit{}, "dev_a", 100)
	released, err := ReleaseOf(c, "dev_a", c.Lease.Fence, 0)
	if err != nil {
		t.Fatal(err)
	}
	if released.Lease.Expires != 0 || released.Lease.ReleasedAt != 0 {
		t.Fatal("background preparation announced a quit")
	}
}
